package cmd

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter"
	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter/config"
	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter/http"
	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter/tracker"
	"github.com/aauren/rtorrent/rtorrent"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/klog/v2"
)

var (
	// Used for cobra / viper configuration
	cfgFile     string
	writeConfig bool

	rootCmd = &cobra.Command{
		Use:   "rtorrent-exporter",
		Short: "A Prometheus exporter for rTorrent",
		Long: `A Prometheus exporter for rTorrent that collects metrics such as download and upload rates, total downloaded and uploaded
		bytes, and tracker information.`,
		Run: RunRoot,
	}

	rootConfig = &config.Config{}
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

//nolint:gochecknoinits // Cobra requires init function to setup flags
func init() {
	fs := flag.NewFlagSet("", flag.PanicOnError)
	klog.InitFlags(fs)
	rootCmd.Flags().AddGoFlagSet(fs)

	cobra.OnInitialize(initConfig)

	// Setup config file parsing
	rootCmd.Flags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.rtorrent-exporter.yaml)")
	rootCmd.Flags().BoolVar(&writeConfig, "write-config", false, "write the current configuration to the config file and exit")
	rootCmd.Flags().Bool("viper", true, "use Viper for configuration")
	_ = viper.BindPFlag("useViper", rootCmd.Flags().Lookup("viper"))

	// Setup telemetry flags
	rootCmd.Flags().StringVar(&rootConfig.Telemetry.Addr, "telemetry.addr", ":9135", "host:port for rTorrent exporter")
	_ = viper.BindPFlag("telemetry.addr", rootCmd.Flags().Lookup("telemetry.addr"))
	rootCmd.Flags().StringVar(&rootConfig.Telemetry.Path, "telemetry.path", "/metrics", "URL path for surfacing collected metrics")
	_ = viper.BindPFlag("telemetry.path", rootCmd.Flags().Lookup("telemetry.path"))
	rootCmd.Flags().DurationVar(&rootConfig.Telemetry.Timeout, "telemetry.timeout", 10*time.Second,
		"[optional] duration of how long to wait to receive http headers on telemetry addr")
	_ = viper.BindPFlag("telemetry.timeout", rootCmd.Flags().Lookup("telemetry.timeout"))

	// Setup rTorrent client connection flags
	rootCmd.Flags().StringVar(&rootConfig.Rtorrent.Addr, "rtorrent.addr", "", "address of rTorrent XML-RPC server")
	_ = viper.BindPFlag("rtorrent.addr", rootCmd.Flags().Lookup("rtorrent.addr"))
	rootCmd.Flags().StringVar(&rootConfig.Rtorrent.Username, "rtorrent.username", "",
		"[optional] username used for HTTP Basic authentication with rTorrent XML-RPC server")
	_ = viper.BindPFlag("rtorrent.username", rootCmd.Flags().Lookup("rtorrent.username"))
	rootCmd.Flags().StringVar(&rootConfig.Rtorrent.Password, "rtorrent.password", "",
		"[optional] password used for HTTP Basic authentication with rTorrent XML-RPC server")
	_ = viper.BindPFlag("rtorrent.password", rootCmd.Flags().Lookup("rtorrent.password"))
	rootCmd.Flags().BoolVar(&rootConfig.Rtorrent.Insecure, "rtorrent.insecure", false,
		"[optional] allow using XML-RPC with a non-CA signed certificat (defaults: false)")
	_ = viper.BindPFlag("rtorrent.insecure", rootCmd.Flags().Lookup("rtorrent.insecure"))
	rootCmd.Flags().DurationVar(&rootConfig.Rtorrent.Timeout, "rtorrent.timeout", 10*time.Second,
		"[optional] duration of how long to wait before timing out rtorrent request")
	_ = viper.BindPFlag("rtorrent.timeout", rootCmd.Flags().Lookup("rtorrent.timeout"))

	// Setup rTorrent exporter collection flags
	rootCmd.Flags().BoolVar(&rootConfig.Rtorrent.Downloads.Collect.Details, "rtorrent.downloads.collect.details", true,
		"[optional] collect rate and total bytes for each torrent (greatly increases metric cardinality)")
	_ = viper.BindPFlag("rtorrent.downloads.collect.details", rootCmd.Flags().Lookup("rtorrent.downloads.collect.details"))
	rootCmd.Flags().BoolVar(&rootConfig.Rtorrent.Downloads.Collect.Messages, "rtorrent.downloads.collect.messages", true,
		"[optional] collect messages for each torrent (greatly increases metric cardinality)")
	_ = viper.BindPFlag("rtorrent.downloads.collect.messages", rootCmd.Flags().Lookup("rtorrent.downloads.collect.messages"))

	// Setup rtorrent tracker collection flags
	rootCmd.Flags().BoolVar(&rootConfig.Rtorrent.Trackers.Enabled, "rtorrent.trackers.enabled", true,
		"[optional] enable tracking of tracker information (increases metric cardinality and load on rTorrent server)")
	_ = viper.BindPFlag("rtorrent.trackers.enabled", rootCmd.Flags().Lookup("rtorrent.trackers.enabled"))
	rootCmd.Flags().DurationVar(&rootConfig.Rtorrent.Trackers.Cache.MinAge, "rtorrent.trackers.cache.min-age", 10*time.Minute,
		"[optional] minimum age of a cached tracker before it is considered for refresh")
	_ = viper.BindPFlag("rtorrent.trackers.cache.min-age", rootCmd.Flags().Lookup("rtorrent.trackers.cache.min-age"))
	rootCmd.Flags().DurationVar(&rootConfig.Rtorrent.Trackers.Cache.MaxAge, "rtorrent.trackers.cache.max-age", 1*time.Hour,
		"[optional] maximum age of a cached tracker before it is considered stale")
	_ = viper.BindPFlag("rtorrent.trackers.cache.max-age", rootCmd.Flags().Lookup("rtorrent.trackers.cache.max-age"))
	rootCmd.Flags().IntVar(&rootConfig.Rtorrent.Trackers.Cache.MaxParallelRequests, "rtorrent.trackers.cache.max-parallel-requests", 5,
		"[optional] maximum number of parallel requests that will be made to rtorrent at a time for fetching tracker information")
	_ = viper.BindPFlag("rtorrent.trackers.cache.max-parallel-requests",
		rootCmd.Flags().Lookup("rtorrent.trackers.cache.max-parallel-requests"))

	rootCmd.AddCommand(versionCmd)
}

func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".cobra" (without extension).
		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".rtorrent-exporter")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		klog.Infof("Using config file: %v", viper.ConfigFileUsed())
	}
	if rootConfig.UseViper || viper.GetBool("useViper") {
		klog.Infof("using viper for configuration")
		err := viper.Unmarshal(rootConfig)
		if err != nil {
			klog.Fatalf("failed to unmarshal configuration: %v", err)
		}
	}
}

func RunRoot(cmd *cobra.Command, args []string) {

	// Validate arguments passed
	klog.V(1).Info("validating flags")
	validateFlags()

	if writeConfig {
		klog.Info("writing configuration to file")
		err := viper.SafeWriteConfig()
		if err != nil {
			klog.Fatalf("failed to write configuration to file: %v", err)
		}
		klog.Infof("configuration written to file: %s", viper.ConfigFileUsed())
		os.Exit(0)
	}

	// Setup context and wait group for graceful shutdown
	primaryCtx, masterCancel := context.WithCancel(context.Background())
	primaryWG := &sync.WaitGroup{}

	// Setup HTTP Client for rtorrent XMLRPC interaction over HTTP
	hcOpts := &http.ClientOpts{
		DialTimeout: rootConfig.Rtorrent.Timeout,
		Insecure:    rootConfig.Rtorrent.Insecure,
		Password:    rootConfig.Rtorrent.Password,
		Username:    rootConfig.Rtorrent.Username,
	}
	rt := http.NewRoundTripper(*hcOpts)

	// Setup rtorrent client
	klog.V(1).Info("creating rTorrent client")
	c, err := rtorrent.New(rootConfig.Rtorrent.Addr, *rt)
	if err != nil {
		klog.Fatalf("cannot create rTorrent client: %v", err)
	}

	// If tracker collection is enabled, then setup the tracker cacher and run it
	var cacher *tracker.Cacher
	if rootConfig.Rtorrent.Trackers.Enabled {
		klog.Info("tracker tracking enabled, setting up tracker cacher")
		ts := &rtorrent.TrackerService{C: c}
		cacher = tracker.NewCacher(ts, tracker.CacheOpts{
			MaxAge:                   rootConfig.Rtorrent.Trackers.Cache.MaxAge,
			MinAge:                   rootConfig.Rtorrent.Trackers.Cache.MinAge,
			MaxParallelRequests:      rootConfig.Rtorrent.Trackers.Cache.MaxParallelRequests,
			TrackerNameSubstitutions: rootConfig.Rtorrent.Trackers.TrackerNameSubstitutions,
		})

		klog.Info("starting tracker cacher")
		primaryWG.Add(1)
		go cacher.Run(primaryCtx, primaryWG)
	}

	// Setup HTTP server for metrics and run it
	mhOpts := http.MetricHandlerOpts{
		MetricsPath:    rootConfig.Telemetry.Path,
		MetricsAddr:    rootConfig.Telemetry.Addr,
		MetricsTimeout: rootConfig.Telemetry.Timeout,
	}
	mh := http.NewMetricHandler(mhOpts)
	primaryWG.Add(1)
	klog.Info("starting HTTP server for metrics")
	go mh.Run(primaryCtx, primaryWG)

	// Setup download collector & pre-warm any caches that may exist
	klog.Info("setting up rTorrent exporter metrics")
	colOpts := rtorrentexporter.CollectorOpts{
		DownloadDetails:    rootConfig.Rtorrent.Downloads.Collect.Details,
		DownloadMessages:   rootConfig.Rtorrent.Downloads.Collect.Messages,
		CollectTrackerInfo: rootConfig.Rtorrent.Trackers.Enabled,
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
	authEnabled := rootConfig.Rtorrent.Username != "" && rootConfig.Rtorrent.Password != ""
	klog.Infof("starting rTorrent exporter on %q for server %q (telemetry timeout: %v) "+
		"(authentication: %v) (insecure: %v) (timeout: %v) (collect download details: %v) (collect messages: %v)"+
		"(collect tracker info: %v) (tracker cache min age: %v) (tracker cache max age: %v) (tracker cache max parallel requests: %v)",
		rootConfig.Telemetry.Path, rootConfig.Telemetry.Addr, rootConfig.Telemetry.Timeout,
		authEnabled, rootConfig.Rtorrent.Addr, rootConfig.Rtorrent.Timeout, rootConfig.Rtorrent.Downloads.Collect.Details,
		rootConfig.Rtorrent.Downloads.Collect.Messages, rootConfig.Rtorrent.Trackers.Enabled, rootConfig.Rtorrent.Trackers.Cache.MinAge,
		rootConfig.Rtorrent.Trackers.Cache.MaxAge, rootConfig.Rtorrent.Trackers.Cache.MaxParallelRequests,
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
	if rootConfig.Rtorrent.Addr == "" {
		klog.Fatal("address of rTorrent XML-RPC server must be specified with '--rtorrent.addr' flag")
	}
	if rootConfig.Rtorrent.Timeout <= 0 {
		klog.Fatal("timeout for rTorrent request must be greater than 0")
	}
	if rootConfig.Telemetry.Timeout <= 0 {
		klog.Fatal("timeout for telemetry request must be greater than 0")
	}

	// Validate tracker settings
	if rootConfig.Rtorrent.Trackers.Enabled && !rootConfig.Rtorrent.Downloads.Collect.Details {
		klog.Fatal("collecting tracker information requires collecting download details, please either disable rtorrent.trackers.enabled " +
			"or enable rtorrent.downloads.collect.details")
	}
	if rootConfig.Rtorrent.Trackers.Enabled {
		if rootConfig.Rtorrent.Trackers.Cache.MinAge <= 0 {
			klog.Fatal("minimum age of a cached tracker must be greater than 0")
		}
		if rootConfig.Rtorrent.Trackers.Cache.MaxAge <= 0 {
			klog.Fatal("maximum age of a cached tracker must be greater than 0")
		}
		if rootConfig.Rtorrent.Trackers.Cache.MaxParallelRequests <= 0 {
			klog.Fatal("maximum number of parallel requests that will be made to rtorrent at a time for fetching tracker information " +
				"must be greater than 0")
		}

		// Attempt to compile all regex matchers
		for i := range rootConfig.Rtorrent.Trackers.TrackerNameSubstitutions {
			tns := &rootConfig.Rtorrent.Trackers.TrackerNameSubstitutions[i]
			if tns.ConvertTo == "" {
				klog.Fatalf("tracker name substitution %v must have a 'convert-to' value", i)
			}
			for _, m := range tns.Matchers {
				r, err := regexp.Compile(m)
				if err != nil {
					klog.Fatalf("failed to compile regexp for tracker (%s) name substitution %v (index %d): %v",
						tns.ConvertTo, m, i, err)
				}
				tns.CompiledMathers = append(tns.CompiledMathers, r)
			}
		}
	}
}
