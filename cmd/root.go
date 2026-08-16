package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	nethttp "net/http"

	//nolint:gosec // pprof still needs to be imported despite what gosec thinks
	_ "net/http/pprof"
	"os"
	"os/signal"
	"regexp"
	"strings"
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
		RunE: RunRoot,
		// main reports the error and picks the exit code, so we don't want cobra printing it again, nor dumping usage for what is almost
		// always a runtime failure rather than a mistake at the command line
		SilenceErrors: true,
		SilenceUsage:  true,
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
	rootCmd.Flags().StringVar(&rootConfig.Telemetry.Password, "telemetry.password", "",
		"Password to be used for basic authentication to the metrics endpoint")
	_ = viper.BindPFlag("telemetry.password", rootCmd.Flags().Lookup("telemetry.password"))
	rootCmd.Flags().StringVar(&rootConfig.Telemetry.Path, "telemetry.path", "/metrics",
		"URL path for surfacing collected metrics")
	_ = viper.BindPFlag("telemetry.path", rootCmd.Flags().Lookup("telemetry.path"))
	rootCmd.Flags().DurationVar(&rootConfig.Telemetry.Timeout, "telemetry.timeout", 10*time.Second,
		"[optional] duration of how long to wait to receive http headers on telemetry addr")
	_ = viper.BindPFlag("telemetry.timeout", rootCmd.Flags().Lookup("telemetry.timeout"))
	rootCmd.Flags().StringVar(&rootConfig.Telemetry.Username, "telemetry.username", "",
		"Username to be used for basic authentication to the metrics endpoint")
	_ = viper.BindPFlag("telemetry.username", rootCmd.Flags().Lookup("telemetry.username"))
	rootCmd.Flags().BoolVar(&rootConfig.Telemetry.EnablePProf, "telemetry.enable-pprof", false,
		"[optional] enable pprof endpoints on rtorrent-exporter (for advanced debugging)")
	_ = viper.BindPFlag("telemetry.enable-pprof", rootCmd.Flags().Lookup("telemetry.enable-pprof"))

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

func RunRoot(cmd *cobra.Command, args []string) error {
	// Validate arguments passed
	klog.V(1).Info("validating flags")
	if err := validateFlags(); err != nil {
		return err
	}

	// Enable pprof for advanced debugging early if requested
	if rootConfig.Telemetry.EnablePProf {
		go func() {
			klog.Infof("starting pprof server on %q", "localhost:6060")
			//nolint:gosec // pprof is a debugging tool we don't care about timeouts
			klog.Info(nethttp.ListenAndServe("0.0.0.0:6060", nil))
		}()
	}

	if writeConfig {
		klog.Info("writing configuration to file")
		if err := viper.SafeWriteConfig(); err != nil {
			return fmt.Errorf("failed to write configuration to file: %w", err)
		}
		klog.Infof("configuration written to file: %s", viper.ConfigFileUsed())
		return nil
	}

	// Setup context and wait group for graceful shutdown, the context cancels itself on SIGINT or SIGTERM
	primaryCtx, masterCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	primaryWG := &sync.WaitGroup{}
	// Registered before the cancel so that it runs after it, which is what lets an early error return stop the children and then wait on
	// them rather than abandoning them mid-flight
	defer primaryWG.Wait()
	defer masterCancel()

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
	c, err := rtorrent.New(rootConfig.Rtorrent.Addr, rt)
	if err != nil {
		return fmt.Errorf("cannot create rTorrent client: %w", err)
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
		primaryWG.Go(func() {
			cacher.Run(primaryCtx)
		})
	}

	// Setup HTTP server for metrics and run it
	mhOpts := http.MetricHandlerOpts{
		MetricsAddr:    rootConfig.Telemetry.Addr,
		MetricsPass:    rootConfig.Telemetry.Password,
		MetricsPath:    rootConfig.Telemetry.Path,
		MetricsTimeout: rootConfig.Telemetry.Timeout,
		MetricsUser:    rootConfig.Telemetry.Username,
	}
	mh := http.NewMetricHandler(mhOpts)
	klog.Info("starting HTTP server for metrics")
	// Written by the goroutine below and only read after primaryWG.Wait(), which is what makes that read safe
	var metricsErr error
	primaryWG.Go(func() {
		if err := mh.Run(primaryCtx); err != nil {
			metricsErr = err
			// An exporter that can't serve metrics has nothing useful left to do, so take the rest of the process down with it
			masterCancel()
		}
	})

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
		return fmt.Errorf("failed to pre-warm caches successfully: %w", err)
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

	// Wait for SIGINT or SIGTERM
	<-primaryCtx.Done()
	klog.Info("shutting down rTorrent exporter")
	primaryWG.Wait()
	if metricsErr != nil {
		return metricsErr
	}

	klog.Info("rTorrent exporter shutdown successfully")
	return nil
}

func validateFlags() error {
	if rootConfig.Rtorrent.Addr == "" {
		return errors.New("address of rTorrent XML-RPC server must be specified with '--rtorrent.addr' flag")
	}
	if rootConfig.Rtorrent.Timeout <= 0 {
		return errors.New("timeout for rTorrent request must be greater than 0")
	}
	if rootConfig.Telemetry.Timeout <= 0 {
		return errors.New("timeout for telemetry request must be greater than 0")
	}
	// The metrics path ends up in a ServeMux method pattern, which panics at registration time on a malformed path, so we reject it here
	// with a proper error instead
	if !strings.HasPrefix(rootConfig.Telemetry.Path, "/") {
		return errors.New("telemetry path must begin with '/', please check the '--telemetry.path' flag")
	}
	if strings.ContainsAny(rootConfig.Telemetry.Path, " \t{}") {
		return errors.New("telemetry path must not contain whitespace or braces, please check the '--telemetry.path' flag")
	}

	// Validate telemetry settings
	telemetryUserSet := rootConfig.Telemetry.Username != ""
	telemetryPassSet := rootConfig.Telemetry.Password != ""
	if telemetryUserSet != telemetryPassSet {
		return errors.New("telemetry basic authentication requires both '--telemetry.username' and '--telemetry.password' to be set " +
			"(or neither)")
	}

	// Validate tracker settings
	rtorrentUserSet := rootConfig.Rtorrent.Username != ""
	rtorrentPassSet := rootConfig.Rtorrent.Password != ""
	if rtorrentUserSet != rtorrentPassSet {
		return errors.New("rTorrent basic authentication requires both '--rtorrent.username' and '--rtorrent.password' to be set " +
			"(or neither)")
	}

	if rootConfig.Rtorrent.Trackers.Enabled && !rootConfig.Rtorrent.Downloads.Collect.Details {
		return errors.New("collecting tracker information requires collecting download details, please either disable " +
			"rtorrent.trackers.enabled or enable rtorrent.downloads.collect.details")
	}
	if !rootConfig.Rtorrent.Trackers.Enabled {
		return nil
	}

	if rootConfig.Rtorrent.Trackers.Cache.MinAge <= 0 {
		return errors.New("minimum age of a cached tracker must be greater than 0")
	}
	if rootConfig.Rtorrent.Trackers.Cache.MaxAge <= 0 {
		return errors.New("maximum age of a cached tracker must be greater than 0")
	}
	if rootConfig.Rtorrent.Trackers.Cache.MaxParallelRequests <= 0 {
		return errors.New("maximum number of parallel requests that will be made to rtorrent at a time for fetching tracker " +
			"information must be greater than 0")
	}

	// Attempt to compile all regex matchers
	for i := range rootConfig.Rtorrent.Trackers.TrackerNameSubstitutions {
		tns := &rootConfig.Rtorrent.Trackers.TrackerNameSubstitutions[i]
		if tns.ConvertTo == "" {
			return fmt.Errorf("tracker name substitution %v must have a 'convert-to' value", i)
		}
		for _, m := range tns.Matchers {
			r, err := regexp.Compile(m)
			if err != nil {
				return fmt.Errorf("failed to compile regexp for tracker (%s) name substitution %v (index %d): %w",
					tns.ConvertTo, m, i, err)
			}
			tns.CompiledMathers = append(tns.CompiledMathers, r)
		}
	}

	return nil
}
