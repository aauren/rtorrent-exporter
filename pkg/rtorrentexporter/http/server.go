package http

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

type MetricHandler struct {
	Server *http.Server
}

type MetricHandlerOpts struct {
	MetricsPath    string
	MetricsAddr    string
	MetricsTimeout time.Duration
}

func NewMetricHandler(opts MetricHandlerOpts) *MetricHandler {
	// Create a new mux instead of the default mux
	mux := http.NewServeMux()

	// Optionally enable HTTP Basic authentication
	mux.Handle(opts.MetricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, opts.MetricsPath, http.StatusMovedPermanently)
	})

	// Configure HTTP Server
	server := &http.Server{
		Addr:              opts.MetricsAddr,
		ReadHeaderTimeout: opts.MetricsTimeout,
		Handler:           mux,
	}

	return &MetricHandler{
		Server: server,
	}
}

func (m *MetricHandler) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	klog.Infof("Starting HTTP server on %s", m.Server.Addr)

	go func() {
		if err := m.Server.ListenAndServe(); err != nil {
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
