package rtorrentexporter

import (
	"fmt"
	"log"

	"github.com/aauren/rtorrent/rtorrent"
	"github.com/prometheus/client_golang/prometheus"
)

var _ DownloadsSource = &rtorrent.DownloadService{}

// A DownloadsSource is a type which can retrieve downloads information from
// rTorrent.  It is implemented by *rtorrent.DownloadService.
type DownloadsSource interface {
	All() ([]string, error)
	Started() ([]string, error)
	Stopped() ([]string, error)
	Complete() ([]string, error)
	Incomplete() ([]string, error)
	Hashing() ([]string, error)
	Seeding() ([]string, error)
	Leeching() ([]string, error)
	Active() ([]string, error)
	ActiveWithDetails() ([][]any, error)

	BaseFilename(infoHash string) (string, error)
	DownloadRate(infoHash string) (int, error)
	DownloadTotal(infoHash string) (int, error)
	UploadRate(infoHash string) (int, error)
	UploadTotal(infoHash string) (int, error)
}

// A DownloadsCollector is a Prometheus collector for metrics regarding rTorrent
// downloads.
type DownloadsCollector struct {
	Downloads           *prometheus.Desc
	DownloadsStarted    *prometheus.Desc
	DownloadsStopped    *prometheus.Desc
	DownloadsComplete   *prometheus.Desc
	DownloadsIncomplete *prometheus.Desc
	DownloadsHashing    *prometheus.Desc
	DownloadsSeeding    *prometheus.Desc
	DownloadsLeeching   *prometheus.Desc
	DownloadsActive     *prometheus.Desc

	DownloadRateBytes  *prometheus.Desc
	DownloadTotalBytes *prometheus.Desc
	UploadRateBytes    *prometheus.Desc
	UploadTotalBytes   *prometheus.Desc

	ds DownloadsSource

	collectActiveDown bool
}

// Verify that DownloadsCollector implements the prometheus.Collector interface.
var _ prometheus.Collector = &DownloadsCollector{}

// NewDownloadsCollector creates a new DownloadsCollector which collects metrics
// regarding rTorrent downloads.
func NewDownloadsCollector(ds DownloadsSource, collectActive bool) *DownloadsCollector {
	const (
		subsystem = "downloads"
	)

	var (
		labels = []string{"info_hash", "name"}
	)

	downCollector := &DownloadsCollector{
		Downloads: prometheus.NewDesc(
			// Subsystem is used as name so we get "rtorrent_downloads"
			prometheus.BuildFQName(namespace, "", subsystem),
			"Total number of downloads.",
			nil,
			nil,
		),

		DownloadsStarted: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "started"),
			"Number of started downloads.",
			nil,
			nil,
		),

		DownloadsStopped: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "stopped"),
			"Number of stopped downloads.",
			nil,
			nil,
		),

		DownloadsComplete: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "complete"),
			"Number of complete downloads.",
			nil,
			nil,
		),

		DownloadsIncomplete: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "incomplete"),
			"Number of incomplete downloads.",
			nil,
			nil,
		),

		DownloadsHashing: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "hashing"),
			"Number of hashing downloads.",
			nil,
			nil,
		),

		DownloadsSeeding: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "seeding"),
			"Number of seeding downloads.",
			nil,
			nil,
		),

		DownloadsLeeching: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "leeching"),
			"Number of leeching downloads.",
			nil,
			nil,
		),

		DownloadsActive: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "active"),
			"Number of active downloads.",
			nil,
			nil,
		),

		ds: ds,

		collectActiveDown: collectActive,
	}

	if collectActive {
		downCollector.DownloadRateBytes = prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "download_rate_bytes"),
			"Current download rate in bytes.",
			labels,
			nil,
		)

		downCollector.DownloadTotalBytes = prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "download_total_bytes"),
			"Total Bytes downloaded.",
			labels,
			nil,
		)

		downCollector.UploadRateBytes = prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "upload_rate_bytes"),
			"Current upload rate in bytes.",
			labels,
			nil,
		)

		downCollector.UploadTotalBytes = prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "upload_total_bytes"),
			"Total Bytes uploaded.",
			labels,
			nil,
		)
	}

	return downCollector
}

// collect begins a metrics collection task for all metrics related to rTorrent
// downloads.
func (c *DownloadsCollector) collect(ch chan<- prometheus.Metric) (*prometheus.Desc, error) {
	if desc, err := c.collectDownloadCounts(ch); err != nil {
		return desc, err
	}

	if c.collectActiveDown {
		if desc, err := c.collectActiveDownloads(ch); err != nil {
			return desc, err
		}
	}

	return nil, nil
}

// collectDownloadCounts collects metrics which track number of downloads in
// various possible states.
func (c *DownloadsCollector) collectDownloadCounts(ch chan<- prometheus.Metric) (*prometheus.Desc, error) {
	all, err := c.ds.All()
	if err != nil {
		return c.Downloads, err
	}

	started, err := c.ds.Started()
	if err != nil {
		return c.DownloadsStarted, err
	}

	stopped, err := c.ds.Stopped()
	if err != nil {
		return c.DownloadsStopped, err
	}

	complete, err := c.ds.Complete()
	if err != nil {
		return c.DownloadsComplete, err
	}

	incomplete, err := c.ds.Incomplete()
	if err != nil {
		return c.DownloadsIncomplete, err
	}

	hashing, err := c.ds.Hashing()
	if err != nil {
		return c.DownloadsHashing, err
	}

	seeding, err := c.ds.Seeding()
	if err != nil {
		return c.DownloadsSeeding, err
	}

	leeching, err := c.ds.Leeching()
	if err != nil {
		return c.DownloadsLeeching, err
	}

	ch <- prometheus.MustNewConstMetric(
		c.Downloads,
		prometheus.GaugeValue,
		float64(len(all)),
	)

	ch <- prometheus.MustNewConstMetric(
		c.DownloadsStarted,
		prometheus.GaugeValue,
		float64(len(started)),
	)

	ch <- prometheus.MustNewConstMetric(
		c.DownloadsStopped,
		prometheus.GaugeValue,
		float64(len(stopped)),
	)

	ch <- prometheus.MustNewConstMetric(
		c.DownloadsComplete,
		prometheus.GaugeValue,
		float64(len(complete)),
	)

	ch <- prometheus.MustNewConstMetric(
		c.DownloadsIncomplete,
		prometheus.GaugeValue,
		float64(len(incomplete)),
	)

	ch <- prometheus.MustNewConstMetric(
		c.DownloadsHashing,
		prometheus.GaugeValue,
		float64(len(hashing)),
	)

	ch <- prometheus.MustNewConstMetric(
		c.DownloadsSeeding,
		prometheus.GaugeValue,
		float64(len(seeding)),
	)

	ch <- prometheus.MustNewConstMetric(
		c.DownloadsLeeching,
		prometheus.GaugeValue,
		float64(len(leeching)),
	)

	return nil, nil
}

// collectActiveDownloads collects information about active downloads,
// which are uploading and/or downloading data.
func (c *DownloadsCollector) collectActiveDownloads(ch chan<- prometheus.Metric) (*prometheus.Desc, error) {
	active, err := c.ds.ActiveWithDetails()
	if err != nil {
		return c.DownloadsActive, err
	}

	ch <- prometheus.MustNewConstMetric(
		c.DownloadsActive,
		prometheus.GaugeValue,
		float64(len(active)),
	)

	// Here active should be a slice of slices, where each inner slice looks like:
	// [hash, name, down.rate, down.total, up.rate, up.total]
	for _, a := range active {
		hash, ok := a[0].(string)
		if !ok {
			return c.DownloadRateBytes, err
		}
		name, ok := a[1].(string)
		if !ok {
			return c.DownloadRateBytes, err
		}

		labels := []string{
			hash,
			name,
		}

		down, ok := a[2].(int64)
		if !ok {
			return c.DownloadRateBytes, fmt.Errorf("failed to convert Download Rate Bytes")
		}

		downTotal, ok := a[3].(int64)
		if !ok {
			return c.DownloadTotalBytes, fmt.Errorf("failed to convert Download Total Bytes")
		}

		up, ok := a[4].(int64)
		if !ok {
			return c.UploadRateBytes, fmt.Errorf("failed to convert Upload Rate Bytes")
		}

		upTotal, ok := a[5].(int64)
		if !ok {
			return c.UploadTotalBytes, fmt.Errorf("failed to convert Upload Total Bytes")
		}

		ch <- prometheus.MustNewConstMetric(
			c.DownloadRateBytes,
			prometheus.GaugeValue,
			float64(down),
			labels...,
		)

		ch <- prometheus.MustNewConstMetric(
			c.DownloadTotalBytes,
			prometheus.GaugeValue,
			float64(downTotal),
			labels...,
		)

		ch <- prometheus.MustNewConstMetric(
			c.UploadRateBytes,
			prometheus.GaugeValue,
			float64(up),
			labels...,
		)

		ch <- prometheus.MustNewConstMetric(
			c.UploadTotalBytes,
			prometheus.GaugeValue,
			float64(upTotal),
			labels...,
		)
	}

	return nil, nil
}

// Describe sends the descriptors of each metric over to the provided channel.
// The corresponding metric values are sent separately.
func (c *DownloadsCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.Downloads,
		c.DownloadsStarted,
		c.DownloadsStopped,
		c.DownloadsComplete,
		c.DownloadsIncomplete,
		c.DownloadsHashing,
		c.DownloadsSeeding,
		c.DownloadsLeeching,
		c.DownloadsActive,

		c.DownloadRateBytes,
		c.DownloadTotalBytes,
		c.UploadRateBytes,
		c.UploadTotalBytes,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect sends the metric values for each metric pertaining to the rTorrent
// downloads to the provided prometheus Metric channel.
func (c *DownloadsCollector) Collect(ch chan<- prometheus.Metric) {
	if desc, err := c.collect(ch); err != nil {
		log.Printf("[ERROR] failed collecting download metric %v: %v", desc, err)
		ch <- prometheus.NewInvalidMetric(desc, err)
		return
	}
}
