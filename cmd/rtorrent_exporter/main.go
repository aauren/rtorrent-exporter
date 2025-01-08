package main

// Command rtorrent-exporter provides a Prometheus exporter for rTorrent.

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter"
	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter/http"
	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter/tracker"
	"github.com/aauren/rtorrent/rtorrent"
	"github.com/prometheus/client_golang/prometheus"
	klog "k8s.io/klog/v2"
)

var (
	telemetryAddr    = flag.String("telemetry.addr", ":9135", "host:port for rTorrent exporter")
	metricsPath      = flag.String("telemetry.path", "/metrics", "URL path for surfacing collected metrics")
	telemetryTimeout = flag.Duration("telemetry.timeout", 10*time.Second,
		"[optional] duration of how long to wait to receive http headers on telemetry addr")

	rtorrentAddr     = flag.String("rtorrent.addr", "", "address of rTorrent XML-RPC server")
	rtorrentUsername = flag.String("rtorrent.username", "",
		"[optional] username used for HTTP Basic authentication with rTorrent XML-RPC server")
	rtorrentPassword = flag.String("rtorrent.password", "",
		"[optional] password used for HTTP Basic authentication with rTorrent XML-RPC server")
	rtorrentInsecure = flag.Bool("rtorrent.insecure", false,
		"[optional] allow using XML-RPC with a non-CA signed certificat (defaults: false)")
	rtorrentTimeout = flag.Duration("rtorrent.timeout", 10*time.Second,
		"[optional] duration of how long to wait before timing out rtorrent request")
	rtorrentDownloadsCollectDetails = flag.Bool("rtorrent.downloads.collect.details", true,
		"[optional] collect rate and total bytes for each torrent (greatly increases metric cardinality)")
	rtorrentDownloadsCollectMessages = flag.Bool("rtorrent.downloads.collect.messages", true,
		"[optional] collect messages for each torrent (greatly increases metric cardinality)")
	rtorrentTrackersEnabled = flag.Bool("rtorrent.trackers.enabled", true,
		"[optional] enable tracking of tracker information (increases metric cardinality and load on rTorrent server)")
	rtorrentTrackersCacheMinAge = flag.Duration("rtorrent.trackers.cache.min-age", 10*time.Minute,
		"[optional] minimum age of a cached tracker before it is considered for refresh")
	rtorrentTrackersCacheMaxAge = flag.Duration("rtorrent.trackers.cache.max-age", 1*time.Hour,
		"[optional] maximum age of a cached tracker before it is considered stale")
	rtorrentTrackersCacheMaxParallelRequests = flag.Int("rtorrent.trackers.cache.max-parallel-requests", 5,
		"[optional] maximum number of parallel requests that will be made to rtorrent at a time for fetching tracker information")
)

func main() {
	// Init all flags / parameters to the application and parse them
	klog.InitFlags(nil)
	flag.Parse()

	// Validate arguments passed
	klog.V(1).Info("validating flags")
	validateFlags()

	// Setup context and wait group for graceful shutdown
	primaryCtx, masterCancel := context.WithCancel(context.Background())
	primaryWG := &sync.WaitGroup{}

	// Setup HTTP Client for rtorrent XMLRPC interaction over HTTP
	hcOpts := &http.ClientOpts{
		DialTimeout: *rtorrentTimeout,
		Insecure:    *rtorrentInsecure,
		Password:    *rtorrentPassword,
		Username:    *rtorrentUsername,
	}
	rt := http.NewRoundTripper(*hcOpts)

	// Setup rtorrent client
	klog.V(1).Info("creating rTorrent client")
	c, err := rtorrent.New(*rtorrentAddr, *rt)
	if err != nil {
		klog.Fatalf("cannot create rTorrent client: %v", err)
	}

	// If tracker collection is enabled, then setup the tracker cacher and run it
	var cacher *tracker.Cacher
	if *rtorrentTrackersEnabled {
		klog.Info("tracker tracking enabled, setting up tracker cacher")
		ts := &rtorrent.TrackerService{C: c}
		cacher = tracker.NewCacher(ts, tracker.CacheOpts{
			MaxAge:              *rtorrentTrackersCacheMaxAge,
			MinAge:              *rtorrentTrackersCacheMinAge,
			MaxParallelRequests: *rtorrentTrackersCacheMaxParallelRequests,
		})

		klog.Info("starting tracker cacher")
		primaryWG.Add(1)
		go cacher.Run(primaryCtx, primaryWG)
	}

	// Setup HTTP server for metrics and run it
	mhOpts := http.MetricHandlerOpts{
		MetricsPath:    *metricsPath,
		MetricsAddr:    *telemetryAddr,
		MetricsTimeout: *telemetryTimeout,
	}
	mh := http.NewMetricHandler(mhOpts)
	primaryWG.Add(1)
	klog.Info("starting HTTP server for metrics")
	go mh.Run(primaryCtx, primaryWG)

	// Setup download collector & pre-warm any caches that may exist
	klog.Info("setting up rTorrent exporter metrics")
	colOpts := rtorrentexporter.CollectorOpts{
		DownloadDetails:    *rtorrentDownloadsCollectDetails,
		DownloadMessages:   *rtorrentDownloadsCollectMessages,
		CollectTrackerInfo: *rtorrentTrackersEnabled,
		TC:                 cacher,
	}
	rte := rtorrentexporter.New(c, colOpts)
	klog.Info("pre-warming any caches for rTorrent exporter that may exist")
	err = rte.PreWarmCaches()
	klog.Info("pre-warming caches for rTorrent exporter complete")
	if err != nil {
		klog.Fatalf("failed to pre-warm caches successfully: %v", err)
	}
	prometheus.MustRegister(rte)
	klog.Info("rTorrent exporter metrics setup complete")

	// Output information about the exporter's configuration
	authEnabled := rtorrentPassword != nil && rtorrentUsername != nil && *rtorrentUsername != "" && *rtorrentPassword != ""
	klog.Infof("starting rTorrent exporter on %q for server %q (telemetry timeout: %v) "+
		"(authentication: %v) (insecure: %v) (timeout: %v) (collect download details: %v) (collect messages: %v)"+
		"(collect tracker info: %v) (tracker cache min age: %v) (tracker cache max age: %v) (tracker cache max parallel requests: %v)",
		*telemetryAddr, *rtorrentAddr, *telemetryTimeout,
		authEnabled, *rtorrentInsecure, *rtorrentTimeout, *rtorrentDownloadsCollectDetails,
		*rtorrentDownloadsCollectMessages, *rtorrentTrackersEnabled, *rtorrentTrackersCacheMinAge,
		*rtorrentTrackersCacheMaxAge, *rtorrentTrackersCacheMaxParallelRequests,
	)
	klog.Info("rTorrent exporter started successfully")

	// Handle SIGINT and SIGTERM
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	// Wait for SIGINT or SIGTERM
	<-ch
	klog.Info("shutting down rTorrent exporter")
	masterCancel()
	primaryWG.Wait()

	klog.Info("rTorrent exporter shutdown successfully")
}

func validateFlags() {
	if *rtorrentAddr == "" {
		klog.Fatal("address of rTorrent XML-RPC server must be specified with '-rtorrent.addr' flag")
	}
	if *rtorrentTimeout <= 0 {
		klog.Fatal("timeout for rTorrent request must be greater than 0")
	}
	if *telemetryTimeout <= 0 {
		klog.Fatal("timeout for telemetry request must be greater than 0")
	}

	// Validate tracker settings
	if *rtorrentTrackersEnabled && !*rtorrentDownloadsCollectDetails {
		klog.Fatal("collecting tracker information requires collecting download details, please either disable rtorrent.trackers.enabled " +
			"or enable rtorrent.downloads.collect.details")
	}
	if *rtorrentTrackersEnabled {
		if *rtorrentTrackersCacheMinAge <= 0 {
			klog.Fatal("minimum age of a cached tracker must be greater than 0")
		}
		if *rtorrentTrackersCacheMaxAge <= 0 {
			klog.Fatal("maximum age of a cached tracker must be greater than 0")
		}
		if *rtorrentTrackersCacheMaxParallelRequests <= 0 {
			klog.Fatal("maximum number of parallel requests that will be made to rtorrent at a time for fetching tracker information " +
				"must be greater than 0")
		}
	}
}
