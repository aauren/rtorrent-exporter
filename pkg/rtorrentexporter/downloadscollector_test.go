package rtorrentexporter

import (
	"testing"
	"time"

	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter/tracker"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testHash = "hash1"
	testName = "name1"
)

// MockDownloadsSource is a mock implementation of the DownloadsSource interface.
type MockDownloadsSource struct {
	mock.Mock
}

func (m *MockDownloadsSource) All() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDownloadsSource) Started() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDownloadsSource) Stopped() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDownloadsSource) Complete() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDownloadsSource) Incomplete() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDownloadsSource) Hashing() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDownloadsSource) Seeding() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDownloadsSource) Leeching() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDownloadsSource) Active() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDownloadsSource) BaseFilename(hash string) (string, error) {
	args := m.Called(hash)
	return args.String(0), args.Error(1)
}

func (m *MockDownloadsSource) DownloadRate(hash string) (int, error) {
	args := m.Called(hash)
	return args.Get(0).(int), args.Error(1)
}

func (m *MockDownloadsSource) DownloadTotal(hash string) (int, error) {
	args := m.Called(hash)
	return args.Get(0).(int), args.Error(1)
}

func (m *MockDownloadsSource) UploadRate(hash string) (int, error) {
	args := m.Called(hash)
	return args.Get(0).(int), args.Error(1)
}

func (m *MockDownloadsSource) UploadTotal(hash string) (int, error) {
	args := m.Called(hash)
	return args.Get(0).(int), args.Error(1)
}

func (m *MockDownloadsSource) DownloadWithDetails(cmds []string) ([][]any, error) {
	args := m.Called(cmds)
	return args.Get(0).([][]any), args.Error(1)
}

func TestNewDownloadsCollector(t *testing.T) {
	t.Parallel()
	ds := new(MockDownloadsSource)
	collectorOpts := CollectorOpts{DownloadDetails: true}
	collector := NewDownloadsCollector(ds, collectorOpts)

	assert.NotNil(t, collector)
	assert.Equal(t, collectorOpts.DownloadDetails, collector.collectOpts.DownloadDetails)
}

func TestDownloadsCollector_collectDownloadCounts(t *testing.T) {
	t.Parallel()
	ds := new(MockDownloadsSource)
	ds.On("All").Return([]string{}, nil)
	ds.On("Started").Return([]string{}, nil)
	ds.On("Stopped").Return([]string{}, nil)
	ds.On("Complete").Return([]string{}, nil)
	ds.On("Incomplete").Return([]string{}, nil)
	ds.On("Hashing").Return([]string{}, nil)
	ds.On("Seeding").Return([]string{}, nil)
	ds.On("Leeching").Return([]string{}, nil)
	ds.On("Active").Return([]string{}, nil)

	collector := NewDownloadsCollector(ds, CollectorOpts{})
	ch := make(chan prometheus.Metric)

	go func() {
		defer close(ch)
		desc, err := collector.collectDownloadCounts(ch)
		assert.Nil(t, desc)
		assert.NoError(t, err)
	}()

	for range ch {
		// Consume the channel
	}
}

func TestDownloadsCollector_collectDownloadDetails(t *testing.T) {
	t.Parallel()
	ds := new(MockDownloadsSource)
	cmds := []string{cmdHash, cmdBaseFilename, cmdDownRate, cmdDownTotal, cmdUpRate, cmdUpTotal, cmdMessage}
	ds.On("DownloadWithDetails", cmds).Return([][]any{
		{testHash, testName, int64(100), int64(200), int64(300), int64(400), nil},
	}, nil)

	collector := NewDownloadsCollector(ds, CollectorOpts{DownloadDetails: true})
	ch := make(chan prometheus.Metric)

	go func() {
		defer close(ch)
		desc, err := collector.collectDownloadDetails(ch)
		assert.Nil(t, desc)
		assert.NoError(t, err)
	}()

	for range ch {
		// Consume the channel
	}
}

// The desc that comes back with an error is what the invalid metric gets reported under, so it should be one that the details path
// actually owns rather than one the counts path already emitted successfully
func TestDownloadsCollector_collectDownloadDetails_error(t *testing.T) {
	t.Parallel()
	ds := new(MockDownloadsSource)
	ds.On("DownloadWithDetails", defaultActiveCommands).Return([][]any{}, assert.AnError)

	collector := NewDownloadsCollector(ds, CollectorOpts{DownloadDetails: true})
	ch := make(chan prometheus.Metric, 1)

	desc, err := collector.collectDownloadDetails(ch)
	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, collector.DownloadRateBytes, desc)
}

func TestDownloadsCollector_parseDownloadDetailsMetrics(t *testing.T) {
	t.Parallel()
	collector := NewDownloadsCollector(nil, CollectorOpts{DownloadDetails: true})
	ch := make(chan prometheus.Metric)
	a := []any{testHash, testName, int64(100), int64(200), int64(300), int64(400)}
	cmds := []string{cmdHash, cmdBaseFilename, cmdDownRate, cmdDownTotal, cmdUpRate, cmdUpTotal}

	go func() {
		defer close(ch)
		hasMessage, err := collector.parseDownloadDetailsMetrics(a, cmds, ch)
		assert.NoError(t, err)
		assert.False(t, hasMessage)
	}()

	for range ch {
		// Consume the channel
	}
}

// A row that doesn't line up with the commands we sent is rtorrent misbehaving, and that should be an error rather than a panic that
// takes down the exporter
func TestDownloadsCollector_parseDownloadDetailsMetrics_malformedRow(t *testing.T) {
	t.Parallel()
	cmds := []string{cmdHash, cmdBaseFilename, cmdDownRate, cmdDownTotal, cmdUpRate, cmdUpTotal}
	tests := []struct {
		name string
		row  []any
	}{
		{"empty row", []any{}},
		{"hash only", []any{testHash}},
		{"short row", []any{testHash, testName, int64(100)}},
		{"long row", []any{testHash, testName, int64(100), int64(200), int64(300), int64(400), int64(500)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			collector := NewDownloadsCollector(nil, CollectorOpts{DownloadDetails: true})
			ch := make(chan prometheus.Metric, len(cmds))

			require.NotPanics(t, func() {
				_, err := collector.parseDownloadDetailsMetrics(tt.row, cmds, ch)
				require.Error(t, err)
			})
		})
	}
}

func TestDownloadsCollector_gatherDownloadDetailLabels(t *testing.T) {
	t.Parallel()
	collector := NewDownloadsCollector(nil, CollectorOpts{})
	torSlice := []any{testHash, testName}

	labels, err := collector.gatherDownloadDetailLabels(torSlice)
	require.NoError(t, err)
	assert.Equal(t, []string{testHash, testName}, labels)
}

// A cache miss and a tracker we couldn't parse should land on the same label value, otherwise the same torrent splits into two series
func TestDownloadsCollector_getURLLabel_cacheMiss(t *testing.T) {
	t.Parallel()
	tc := tracker.NewCacher(nil, tracker.CacheOpts{MaxParallelRequests: 1, MinAge: time.Minute, MaxAge: time.Hour})
	collector := NewDownloadsCollector(nil, CollectorOpts{DownloadDetails: true, CollectTrackerInfo: true, TC: tc})

	assert.Equal(t, tracker.UnknownDomain, collector.getURLLabel(testHash))
}

func TestDownloadsCollector_getDownloadDetailCommands(t *testing.T) {
	t.Parallel()
	collector := NewDownloadsCollector(nil, CollectorOpts{})
	cmds := collector.getDownloadDetailCommands()
	assert.Equal(t, defaultActiveCommands, cmds)
}

func TestDownloadsCollector_Describe(t *testing.T) {
	t.Parallel()
	collector := NewDownloadsCollector(nil, CollectorOpts{DownloadDetails: true, DownloadMessages: true})
	ch := make(chan *prometheus.Desc)

	go func() {
		defer close(ch)
		collector.Describe(ch)
	}()

	var descs []string
	for d := range ch {
		descs = append(descs, d.String())
	}

	// Everything Collect can emit should be described, otherwise the two lists drift apart
	for _, want := range []*prometheus.Desc{collector.Downloads, collector.DownloadsError, collector.DownloadMessages} {
		assert.Contains(t, descs, want.String())
	}
}

func mockCountsSource() *MockDownloadsSource {
	ds := new(MockDownloadsSource)
	for _, view := range []string{"All", "Started", "Stopped", "Complete", "Incomplete", "Hashing", "Seeding", "Leeching", "Active"} {
		ds.On(view).Return([]string{testHash}, nil)
	}
	return ds
}

func gatheredNames(t *testing.T, c prometheus.Collector) []string {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(c))
	mfs, err := reg.Gather()
	require.NoError(t, err)

	names := make([]string, 0, len(mfs))
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	return names
}

func TestDownloadsCollector_Collect(t *testing.T) {
	t.Parallel()
	t.Run("total downloads is emitted without details", func(t *testing.T) {
		t.Parallel()
		collector := NewDownloadsCollector(mockCountsSource(), CollectorOpts{})

		assert.Contains(t, gatheredNames(t, collector), "rtorrent_downloads")
	})

	t.Run("total downloads is emitted with details", func(t *testing.T) {
		t.Parallel()
		ds := mockCountsSource()
		ds.On("DownloadWithDetails", defaultActiveCommands).Return([][]any{
			{testHash, testName, int64(100), int64(200), int64(300), int64(400), nil},
		}, nil)
		collector := NewDownloadsCollector(ds, CollectorOpts{DownloadDetails: true})

		assert.Contains(t, gatheredNames(t, collector), "rtorrent_downloads")
	})
}
