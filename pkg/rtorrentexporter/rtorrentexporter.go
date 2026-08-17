// Package rtorrentexporter provides the Exporter type used in the
// rtorrent_exporter Prometheus exporter.
package rtorrentexporter

import (
	"fmt"
	"sync"

	"github.com/aauren/rtorrent/rtorrent"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// namespace is the top-level namespace for this rTorrent exporter.
	namespace = "rtorrent"
)

// CachingCollector is a prometheus.Collector that may be sitting in front of a cache which wants filling before the first scrape arrives.
type CachingCollector interface {
	prometheus.Collector
	// PreWarmCache pre-warms the cache for the collector. If a collector does not have a cache to pre-warm it is expected to be a no-op.
	PreWarmCache() error
}

// An Exporter is a Prometheus exporter for rTorrent metrics.
// It wraps all rTorrent metrics collectors and provides a single global
// exporter which can serve metrics. It also ensures that the collection
// is done in a thread-safe manner, the necessary requirement stated by
// Prometheus. It implements the prometheus.Collector interface in order to
// register with Prometheus.
type Exporter struct {
	mu         sync.Mutex
	collectors []CachingCollector
}

// Verify that the Exporter implements the prometheus.Collector interface.
var _ prometheus.Collector = &Exporter{}

// New creates a new Exporter which collects metrics from one or mote sites.
func New(c rtorrent.Client, collectOpts CollectorOpts) *Exporter {
	ds := &rtorrent.DownloadService{C: c}
	return &Exporter{
		collectors: []CachingCollector{
			NewDownloadsCollector(ds, collectOpts),
		},
	}
}

// Describe sends all the descriptors of the collectors included to
// the provided channel.
func (c *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, cc := range c.collectors {
		cc.Describe(ch)
	}
}

// Collect sends the collected metrics from each of the collectors to
// prometheus. Collect could be called several times concurrently
// and thus its run is protected by a single mutex.
func (c *Exporter) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, cc := range c.collectors {
		cc.Collect(ch)
	}
}

// PreWarmCaches pre-warms caches for all collectors that have them.
func (c *Exporter) PreWarmCaches() error {
	for _, cc := range c.collectors {
		if err := cc.PreWarmCache(); err != nil {
			return fmt.Errorf("pre-warming cache for collector %T: %w", cc, err)
		}
	}
	return nil
}
