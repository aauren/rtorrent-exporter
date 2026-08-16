package http

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

// shutdownTimeout is how long we let in-flight requests drain before giving up on a graceful shutdown
const shutdownTimeout = 10 * time.Second

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

// Run serves metrics until ctx is cancelled or the listener fails, and then shuts the server down. It reports the reason it stopped rather
// than killing the process, because deciding that a failure is fatal is the caller's business and not a library's
func (m *MetricHandler) Run(ctx context.Context) error {
	klog.Infof("Starting HTTP server on %s", m.Server.Addr)

	// Buffered so that the goroutine can always report and exit, even when nobody is left to receive
	srvErr := make(chan error, 1)
	go func() {
		err := m.Server.ListenAndServe()
		// ListenAndServe always returns ErrServerClosed once Shutdown has been called, which is the one outcome that isn't a failure
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		srvErr <- err
	}()

	select {
	case err := <-srvErr:
		if err != nil {
			return fmt.Errorf("HTTP server on %s failed: %w", m.Server.Addr, err)
		}
		return nil
	case <-ctx.Done():
	}

	klog.Infof("We've been asked to stop the HTTP server")
	klog.Infof("Shutting down HTTP server on %s", m.Server.Addr)
	// Bound the drain, because an in-flight scrape that never finishes shouldn't be able to hold shutdown open forever
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := m.Server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("HTTP server shutdown error: %w", err)
	}

	return nil
}
