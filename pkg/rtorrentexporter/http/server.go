package http

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

type MetricHandler struct {
	Server *http.Server
}

type MetricHandlerOpts struct {
	MetricsAddr    string
	MetricsPass    string
	MetricsPath    string
	MetricsTimeout time.Duration
	MetricsUser    string
}

func basicAuth(next http.Handler, user, pass string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		// subtle.ConstantTimeCompare() used below to remove possibility of HTTP timing attacks
		if !ok ||
			subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(pass)) != 1 {
			klog.Warningf("Unauthorized attempt made on endpoint with user: %s", u)
			w.Header().Set("WWW-Authenticate", `Basic realm="restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func NewMetricHandler(opts MetricHandlerOpts) *MetricHandler {
	// Create a new mux instead of the default mux
	mux := http.NewServeMux()

	// Scraping is a read, so we register GET (which also covers HEAD) and let the mux answer anything else with a 405
	mux.Handle("GET "+opts.MetricsPath, promhttp.Handler())
	// Skip the redirect when metrics are already served from the root, because registering both on the same mux would panic
	if opts.MetricsPath != "/" {
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, opts.MetricsPath, http.StatusMovedPermanently)
		})
	}

	var hand http.Handler
	hand = mux
	if opts.MetricsUser != "" && opts.MetricsPass != "" {
		hand = basicAuth(mux, opts.MetricsUser, opts.MetricsPass)
	}

	// Configure HTTP Server
	server := &http.Server{
		Addr:              opts.MetricsAddr,
		ReadHeaderTimeout: opts.MetricsTimeout,
		Handler:           hand,
	}

	return &MetricHandler{
		Server: server,
	}
}

func (m *MetricHandler) Run(ctx context.Context) {
	klog.Infof("Starting HTTP server on %s", m.Server.Addr)

	go func() {
		// ListenAndServe always returns ErrServerClosed once Shutdown has been called, so we can't treat that as fatal or an orderly
		// termination races us into killing the process before Run gets a chance to return
		if err := m.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			klog.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	klog.Infof("We've been asked to stop the HTTP server")
	klog.Infof("Shutting down HTTP server on %s", m.Server.Addr)
	if err := m.Server.Shutdown(context.Background()); err != nil {
		klog.Fatalf("HTTP server shutdown error: %v", err)
	}
}
