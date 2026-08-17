package rtorrentexporter

import (
	"errors"
	"fmt"
	"slices"
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

	// detailDescs maps each download detail command that carries a plain int64 counter to the metric it feeds, so that parsing a
	// download's details is a lookup rather than a case per command. cmdMessage is deliberately absent because it needs real handling.
	detailDescs map[string]*prometheus.Desc

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

// The rTorrent XML-RPC commands that we send when asking for download details
const (
	cmdHash         = "d.hash="
	cmdBaseFilename = "d.base_filename="
	cmdDownRate     = "d.down.rate="
	cmdDownTotal    = "d.down.total="
	cmdUpRate       = "d.up.rate="
	cmdUpTotal      = "d.up.total="
	cmdMessage      = "d.message="
)

var (
	hashOnlyCommand       = []string{cmdHash}
	defaultActiveCommands = []string{cmdHash, cmdBaseFilename, cmdDownRate, cmdDownTotal, cmdUpRate, cmdUpTotal, cmdMessage}
)

// ErrHashConversion is what a caller gets when rTorrent hands back something other than a string where an info hash was expected. It
// escapes as far as PreWarmCaches, so it's worth being matchable
var ErrHashConversion = errors.New("failed to convert torrent hash to string")

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

		downCollector.detailDescs = map[string]*prometheus.Desc{
			cmdDownRate:  downCollector.DownloadRateBytes,
			cmdDownTotal: downCollector.DownloadTotalBytes,
			cmdUpRate:    downCollector.UploadRateBytes,
			cmdUpTotal:   downCollector.UploadTotalBytes,
		}
	}

	if downCollector.collectOpts.DownloadMessages {
		// Concat rather than append, because append would write "message" into labels' spare capacity, which is shared with every
		// other user of labels
		msgLabels := slices.Concat(labels, []string{"message"})
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
	// Every one of these is a separate rTorrent view whose only interesting property is how many entries came back
	views := []struct {
		desc  *prometheus.Desc
		fetch func() ([]string, error)
	}{
		{c.DownloadsStarted, c.ds.Started},
		{c.DownloadsStopped, c.ds.Stopped},
		{c.DownloadsComplete, c.ds.Complete},
		{c.DownloadsIncomplete, c.ds.Incomplete},
		{c.DownloadsHashing, c.ds.Hashing},
		{c.DownloadsSeeding, c.ds.Seeding},
		{c.DownloadsLeeching, c.ds.Leeching},
		{c.DownloadsActive, c.ds.Active},
	}

	// We gather every count before emitting any of them so that a failure part way through doesn't leave the scrape holding a
	// half-populated set
	counts := make([]float64, len(views))
	for i, v := range views {
		entries, err := v.fetch()
		if err != nil {
			return v.desc, err
		}
		counts[i] = float64(len(entries))
	}

	for i, v := range views {
		ch <- prometheus.MustNewConstMetric(v.desc, prometheus.GaugeValue, counts[i])
	}

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
			klog.Errorf("failed to parse download details metrics: %v", err)
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
		switch cmd := abbrCommands[idx]; cmd {
		case cmdMessage:
			// If there are no messages, then just continue
			if v == nil {
				continue
			}

			msg, ok := v.(string)
			if !ok {
				return errorMessage, errors.New("failed to convert Download Message")
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
				// Same aliasing reason as in NewDownloadsCollector, labels is shared by every metric this loop emits, so the
				// message label needs its own backing array
				msgLabels := slices.Concat(labels, []string{msg})
				ch <- prometheus.MustNewConstMetric(
					c.DownloadMessages,
					prometheus.GaugeValue,
					1,
					msgLabels...,
				)
			}
		default:
			// Anything else is a plain int64 counter, and the only thing that varies is which metric it lands on. A command we
			// have no metric for isn't an error, it just means we asked rTorrent for something we don't export.
			desc, ok := c.detailDescs[cmd]
			if !ok {
				continue
			}

			count, ok := v.(int64)
			if !ok {
				return errorMessage, fmt.Errorf("failed to convert the value of %s to an int64", cmd)
			}
			ch <- prometheus.MustNewConstMetric(
				desc,
				prometheus.GaugeValue,
				float64(count),
				labels...,
			)
		}
	}

	return errorMessage, nil
}

// gatherDownloadDetailLabels gathers the labels for a single download.
func (c *DownloadsCollector) gatherDownloadDetailLabels(torSlice []any) ([]string, error) {
	hash, ok := torSlice[0].(string)
	if !ok {
		return nil, ErrHashConversion
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
		return fmt.Errorf("encountered error getting download hashes: %w", err)
	}

	for _, hash := range allDownHashes {
		// Attempt to get the hash out of the slice
		h, ok := hash[0].(string)
		if !ok {
			return ErrHashConversion
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
