package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The handler has to serve whatever registry it was handed, so that RunRoot can fully populate one before a listener exists
func TestNewMetricHandler_servesGivenRegistry(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "rtorrent_test_value", Help: "test"})
	g.Set(42)
	reg.MustRegister(g)

	mh := NewMetricHandler(MetricHandlerOpts{
		MetricsAddr:    "127.0.0.1:0",
		MetricsPath:    "/metrics",
		MetricsTimeout: time.Second,
		Gatherer:       reg,
	})

	rec := httptest.NewRecorder()
	mh.Server.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "rtorrent_test_value 42")
}
