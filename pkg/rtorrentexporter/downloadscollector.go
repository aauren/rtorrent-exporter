package rtorrentexporter

import (
	"fmt"
	"time"

	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter/tracker"
	"github.com/aauren/rtorrent/rtorrent"
	"github.com/prometheus/client_golang/prometheus"
	klog "k8s.io/klog/v2"
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
	DownloadWithDetails([]string) ([][]any, error)

	BaseFilename(infoHash string) (string, error)
	DownloadRate(infoHash string) (int, error)
	DownloadTotal(infoHash string) (int, error)
	UploadRate(infoHash string) (int, error)
	UploadTotal(infoHash string) (int, error)
}

// A DownloadsCollector is a Prometheus collector for metrics regarding rTorrent
// downloads.
type DownloadsCollector struct {
	// Downloads metrics, these are mostly counts of the various states of downloads
	Downloads           *prometheus.Desc
	DownloadsStarted    *prometheus.Desc
	DownloadsStopped    *prometheus.Desc
	DownloadsComplete   *prometheus.Desc
	DownloadsIncomplete *prometheus.Desc
	DownloadsHashing    *prometheus.Desc
	DownloadsSeeding    *prometheus.Desc
	DownloadsLeeching   *prometheus.Desc
	DownloadsActive     *prometheus.Desc
	// This one requres download details to be collected, but is otherwise also a simple count
	DownloadsError *prometheus.Desc

	// Download details metrics, these are the actual download rates and totals
	DownloadRateBytes  *prometheus.Desc
	DownloadTotalBytes *prometheus.Desc
	UploadRateBytes    *prometheus.Desc
	UploadTotalBytes   *prometheus.Desc

	// Download messages, these are the messages that come from the tracker
	DownloadMessages *prometheus.Desc

	// DownloadsSource is the source from which we get the downloads, this is typically provided by a facade or the rtorrent library
	ds DownloadsSource

	// CollectorOpts are the options that the collector was created with
	collectOpts *CollectorOpts
}

type CollectorOpts struct {
	DownloadDetails    bool
	DownloadMessages   bool
	CollectTrackerInfo bool
	TC                 *tracker.Cacher
}

var (
	hashOnlyCommand       = []string{"d.hash="}
	defaultActiveCommands = []string{"d.hash=", "d.base_filename=", "d.down.rate=", "d.down.total=", "d.up.rate=", "d.up.total=",
		"d.message="}
)

// Verify that DownloadsCollector implements the prometheus.Collector interface.
var _ prometheus.Collector = &DownloadsCollector{}

// NewDownloadsCollector creates a new DownloadsCollector which collects metrics
// regarding rTorrent downloads.
func NewDownloadsCollector(ds DownloadsSource, collectorOpts CollectorOpts) *DownloadsCollector {
	const subsystem = "downloads"

	labels := []string{"info_hash", "name"}
	if collectorOpts.CollectTrackerInfo {
		labels = append(labels, "tracker")
	}

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

		collectOpts: &collectorOpts,
	}

	if downCollector.collectOpts.DownloadDetails {
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

		// As errors are reflected by evaluating the details that come from downloads, we're only able to get them if we're collecting,
		// but otherwise this metric is a lot more similar to the view based ones above.
		downCollector.DownloadsError = prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "error"),
			"Number of downloads with tracker errors.",
			nil,
			nil,
		)
	}

	if downCollector.collectOpts.DownloadMessages {
		msgLabels := append(labels, "message")
		downCollector.DownloadMessages = prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "messages"),
			"Tracker messages for downloads.",
			msgLabels,
			nil,
		)
	}

	return downCollector
}

// collect begins a metrics collection task for all metrics related to rTorrent
// downloads.
func (c *DownloadsCollector) collect(ch chan<- prometheus.Metric) (*prometheus.Desc, error) {
	started := time.Now()
	klog.V(1).Info("Collecting downloads metrics")
	if desc, err := c.collectDownloadCounts(ch); err != nil {
		return desc, err
	}

	if c.collectOpts.DownloadDetails {
		klog.V(1).Info("Collecting download details metrics")
		if desc, err := c.collectDownloadDetails(ch); err != nil {
			return desc, err
		}
	}

	klog.V(1).Infof("Finished collecting downloads metrics in %v", time.Since(started))
	return nil, nil
}

// collectDownloadCounts collects metrics which track number of downloads in various possible states.
func (c *DownloadsCollector) collectDownloadCounts(ch chan<- prometheus.Metric) (*prometheus.Desc, error) {
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

	active, err := c.ds.Active()
	if err != nil {
		return c.DownloadsActive, err
	}

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

	ch <- prometheus.MustNewConstMetric(
		c.DownloadsActive,
		prometheus.GaugeValue,
		float64(len(active)),
	)

	return nil, nil
}

// collectDownloadDetails collects information about active downloads, which are uploading and/or downloading data.
func (c *DownloadsCollector) collectDownloadDetails(ch chan<- prometheus.Metric) (*prometheus.Desc, error) {
	cmds := c.getDownloadDetailCommands()

	all, err := c.ds.DownloadWithDetails(cmds)
	if err != nil {
		return c.DownloadsActive, err
	}

	ch <- prometheus.MustNewConstMetric(
		c.Downloads,
		prometheus.GaugeValue,
		float64(len(all)),
	)

	failedDownloads := 0

	// Here active should be a slice of slices, where each inner slice looks like:
	// [hash, name, down.rate, down.total, up.rate, up.total]
	for _, a := range all {
		hadErrorMessage, err := c.parseDownloadDetailsMetrics(a, cmds, ch)
		if err != nil {
			return c.DownloadRateBytes, err
		}
		if hadErrorMessage {
			failedDownloads++
		}
	}

	// Finally emit the total number of errored downloads
	ch <- prometheus.MustNewConstMetric(
		c.DownloadsError,
		prometheus.GaugeValue,
		float64(failedDownloads),
	)

	return nil, nil
}

// parseDownloadDetailsMetrics parses the metrics for a single download and sends them to the provided channel.
func (c *DownloadsCollector) parseDownloadDetailsMetrics(a []any, cmds []string, ch chan<- prometheus.Metric) (bool, error) {
	labels, err := c.gatherDownloadDetailLabels(a)
	if err != nil {
		return false, err
	}

	// collect metrics starting without hash or name (starting at index 2 and beyond), cannot put this directly in range because it creates
	// a new slice starting with index 0 which makes it not able to match the correct command
	abbrA := a[2:]
	abbrCommands := cmds[2:]

	errorMessage := false

	for idx, v := range abbrA {
		switch abbrCommands[idx] {
		case "d.down.rate=":
			down, ok := v.(int64)
			if !ok {
				return errorMessage, fmt.Errorf("failed to convert Download Rate Bytes")
			}
			ch <- prometheus.MustNewConstMetric(
				c.DownloadRateBytes,
				prometheus.GaugeValue,
				float64(down),
				labels...,
			)
		case "d.down.total=":
			downTotal, ok := v.(int64)
			if !ok {
				return errorMessage, fmt.Errorf("failed to convert Download Total Bytes")
			}
			ch <- prometheus.MustNewConstMetric(
				c.DownloadTotalBytes,
				prometheus.GaugeValue,
				float64(downTotal),
				labels...,
			)
		case "d.up.rate=":
			up, ok := v.(int64)
			if !ok {
				return errorMessage, fmt.Errorf("failed to convert Upload Rate Bytes")
			}
			ch <- prometheus.MustNewConstMetric(
				c.UploadRateBytes,
				prometheus.GaugeValue,
				float64(up),
				labels...,
			)
		case "d.up.total=":
			upTotal, ok := v.(int64)
			if !ok {
				return errorMessage, fmt.Errorf("failed to convert Upload Total Bytes")
			}
			ch <- prometheus.MustNewConstMetric(
				c.UploadTotalBytes,
				prometheus.GaugeValue,
				float64(upTotal),
				labels...,
			)
		case "d.message=":
			// If there are no messages, then just continue
			if v == nil {
				continue
			}

			msg, ok := v.(string)
			if !ok {
				return errorMessage, fmt.Errorf("failed to convert Download Message")
			}

			// Excluding message Tried all trackers taken from rutorrent code base as a general exclusion for a tracker message that doesn't
			// really mean that there is an error.
			if msg == "" || msg == "Tracker: [Tried all trackers.]" {
				continue
			}

			// Unfortunately, rtorrent doesn't actually give us a good way of tracking errors, so we have to assume that if there is a
			// tracker message then it has an error
			errorMessage = true

			// Only emit the actual message as a metric if the user has instructed us to do so
			if c.collectOpts.DownloadMessages {
				msgLabels := append(labels, msg)
				ch <- prometheus.MustNewConstMetric(
					c.DownloadMessages,
					prometheus.GaugeValue,
					1,
					msgLabels...,
				)
			}
		}
	}

	return errorMessage, nil
}

// gatherDownloadDetailLabels gathers the labels for a single download.
func (c *DownloadsCollector) gatherDownloadDetailLabels(torSlice []any) ([]string, error) {
	hash, ok := torSlice[0].(string)
	if !ok {
		return nil, fmt.Errorf("failed to convert torrent hash to string")
	}
	name, ok := torSlice[1].(string)
	if !ok {
		return nil, fmt.Errorf("failed to convert torrent name to string, for hash: %s", hash)
	}
	labels := []string{
		hash,
		name,
	}

	// Add the tracker URL to the labels if it is available, otherwise add "unknown" and continue despite errors
	if c.collectOpts.CollectTrackerInfo {
		url := c.getURLLabel(hash)
		labels = append(labels, url)
	}

	return labels, nil
}

// getURLLabel gets the URL label for a given hash.
func (c *DownloadsCollector) getURLLabel(hash string) string {
	t := c.collectOpts.TC.GetTrackerFromCacheNonBlocking(rtorrent.NewTrackerNoIndex(hash))
	if t == nil {
		return ""
	}

	return t.SubstitutedDomain
}

// getDownloadDetailCommands returns the commands to be used for gathering download details.
func (c *DownloadsCollector) getDownloadDetailCommands() []string {
	return defaultActiveCommands
}

// PreWarmCache pre-warms the tracker cache using the hashes of the current downloads.
func (c *DownloadsCollector) PreWarmCache() error {
	// If we're not configured for collecting tracker information, then turn this warming step into a noop
	if !c.collectOpts.CollectTrackerInfo {
		return nil
	}

	// Find all of the hashes for the current downloads
	allDownHashes, err := c.ds.DownloadWithDetails(hashOnlyCommand)
	if err != nil {
		return fmt.Errorf("encountered error getting download hashes: %v", err)
	}

	for _, hash := range allDownHashes {
		// Attempt to get the hash out of the slice
		h, ok := hash[0].(string)
		if !ok {
			return fmt.Errorf("failed to convert hash to string")
		}

		// Send cache request
		c.collectOpts.TC.GetTrackerFromCacheNonBlocking(rtorrent.NewTrackerNoIndex(h))
	}

	return nil
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
	}

	if c.collectOpts.DownloadDetails {
		ds = append(ds,
			c.DownloadRateBytes,
			c.DownloadTotalBytes,
			c.UploadRateBytes,
			c.UploadTotalBytes,
		)
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect sends the metric values for each metric pertaining to the rTorrent
// downloads to the provided prometheus Metric channel.
func (c *DownloadsCollector) Collect(ch chan<- prometheus.Metric) {
	if desc, err := c.collect(ch); err != nil {
		klog.Errorf("[ERROR] failed collecting download metric %v: %v", desc, err)
		ch <- prometheus.NewInvalidMetric(desc, err)
		return
	}
}
