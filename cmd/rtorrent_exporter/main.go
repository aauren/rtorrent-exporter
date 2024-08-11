//nolint:depguard // we haven't configured depguard for this project
package main

// Command rtorrent-exporter provides a Prometheus exporter for rTorrent.

import (
	"crypto/tls"
	"flag"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/aauren/rtorrent/rtorrent"
	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	telemetryAddr    = flag.String("telemetry.addr", ":9135", "host:port for rTorrent exporter")
	metricsPath      = flag.String("telemetry.path", "/metrics", "URL path for surfacing collected metrics")
	telemetryTimeout = flag.Duration("telemetry.timeout", 10*time.Second,
		"[optional] duration of how long to wait to receive http headers on telemetry addr (defaults: 10s)")

	rtorrentAddr     = flag.String("rtorrent.addr", "", "address of rTorrent XML-RPC server")
	rtorrentUsername = flag.String("rtorrent.username", "",
		"[optional] username used for HTTP Basic authentication with rTorrent XML-RPC server")
	rtorrentPassword = flag.String("rtorrent.password", "",
		"[optional] password used for HTTP Basic authentication with rTorrent XML-RPC server")
	rtorrentInsecure = flag.Bool("rtorrent.insecure", false,
		"[optional] allow using XML-RPC with a non-CA signed certificat (defaults: false)")
	rtorrentTimeout = flag.Duration("rtorrent.timeout", 10*time.Second,
		"[optional] duration of how long to wait before timing out rtorrent request (defaults: 10s)")
	rtorrentCollectActive = flag.Bool("rtorrent.collect.active", true,
		"[optional] collect rate and total bytes for each torrent (greatly increases metric cardinality) (defaults: true)")
)

func main() {
	flag.Parse()

	validateFlags()

	// Optionally enable HTTP Basic authentication
	var rt http.RoundTripper
	authEnabled := false
	if u, p := *rtorrentUsername, *rtorrentPassword; u != "" && p != "" {
		rt = &authRoundTripper{
			Username: u,
			Password: p,
			Transport: &http.Transport{
				Dial: dialTimeout,
				TLSClientConfig: &tls.Config{
					//nolint:gosec // we don't care that this may be true, that's the point
					InsecureSkipVerify: *rtorrentInsecure,
				},
			},
		}
		authEnabled = true
	} else {
		rt = &authRoundTripper{
			Transport: &http.Transport{
				Dial: dialTimeout,
				TLSClientConfig: &tls.Config{
					//nolint:gosec // we don't care that this may be true, that's the point
					InsecureSkipVerify: *rtorrentInsecure,
				},
			},
		}
	}

	c, err := rtorrent.New(*rtorrentAddr, rt)
	if err != nil {
		log.Fatalf("cannot create rTorrent client: %v", err)
	}

	prometheus.MustRegister(rtorrentexporter.New(c, *rtorrentCollectActive))

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, *metricsPath, http.StatusMovedPermanently)
	})

	log.Printf("starting rTorrent exporter on %q for server %q (telemetry timeout: %v) "+
		"(authentication: %v) (insecure: %v) (timeout: %v) (collect active: %v)",
		*telemetryAddr, *rtorrentAddr, *telemetryTimeout,
		authEnabled, *rtorrentInsecure, *rtorrentTimeout, *rtorrentCollectActive)

	server := &http.Server{
		Addr:              *telemetryAddr,
		ReadHeaderTimeout: *telemetryTimeout,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("cannot start rTorrent exporter: %s", err)
	}
}

func validateFlags() {
	if *rtorrentAddr == "" {
		log.Fatal("address of rTorrent XML-RPC server must be specified with '-rtorrent.addr' flag")
	}
	if *rtorrentTimeout <= 0 {
		log.Fatal("timeout for rTorrent request must be greater than 0")
	}
	if *telemetryTimeout <= 0 {
		log.Fatal("timeout for telemetry request must be greater than 0")
	}
}

var _ http.RoundTripper = &authRoundTripper{}

// An authRoundTripper is a http.RoundTripper which adds HTTP Basic authentication
// to each HTTP request.
type authRoundTripper struct {
	Username  string
	Password  string
	Transport *http.Transport
}

func dialTimeout(network, addr string) (net.Conn, error) {
	return net.DialTimeout(network, addr, *rtorrentTimeout)
}

func (rt *authRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r.SetBasicAuth(rt.Username, rt.Password)
	return rt.Transport.RoundTrip(r)
}
