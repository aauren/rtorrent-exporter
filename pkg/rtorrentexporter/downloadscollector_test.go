package rtorrentexporter

import (
    "testing"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
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
    ds := new(MockDownloadsSource)
    collectorOpts := CollectorOpts{DownloadDetails: true}
    collector := NewDownloadsCollector(ds, collectorOpts)

    assert.NotNil(t, collector)
    assert.Equal(t, collectorOpts.DownloadDetails, collector.collectOpts.DownloadDetails)
}

func TestDownloadsCollector_collectDownloadCounts(t *testing.T) {
    ds := new(MockDownloadsSource)
    ds.On("All").Return([]string{}, nil)
    ds.On("Started").Return([]string{}, nil)
    ds.On("Stopped").Return([]string{}, nil)
    ds.On("Complete").Return([]string{}, nil)
    ds.On("Incomplete").Return([]string{}, nil)
    ds.On("Hashing").Return([]string{}, nil)
    ds.On("Seeding").Return([]string{}, nil)
    ds.On("Leeching").Return([]string{}, nil)

    collector := NewDownloadsCollector(ds, CollectorOpts{})
    ch := make(chan prometheus.Metric)

    go func() {
        defer close(ch)
        desc, err := collector.collectDownloadCounts(ch)
        assert.Nil(t, desc)
        assert.Nil(t, err)
    }()

    for range ch {
        // Consume the channel
    }
}

func TestDownloadsCollector_collectDownloadDetails(t *testing.T) {
    ds := new(MockDownloadsSource)
    cmds := []string{"d.hash=", "d.base_filename=", "d.down.rate=", "d.down.total=", "d.up.rate=", "d.up.total="}
    ds.On("DownloadWithDetails", cmds).Return([][]any{
        {"hash1", "name1", int64(100), int64(200), int64(300), int64(400)},
    }, nil)

    collector := NewDownloadsCollector(ds, CollectorOpts{DownloadDetails: true})
    ch := make(chan prometheus.Metric)

    go func() {
        defer close(ch)
        desc, err := collector.collectDownloadDetails(ch)
        assert.Nil(t, desc)
        assert.Nil(t, err)
    }()

    for range ch {
        // Consume the channel
    }
}

func TestDownloadsCollector_parseDownloadDetailsMetrics(t *testing.T) {
    collector := NewDownloadsCollector(nil, CollectorOpts{DownloadDetails: true})
    ch := make(chan prometheus.Metric)
    a := []any{"hash1", "name1", int64(100), int64(200), int64(300), int64(400)}
    cmds := []string{"d.down.rate=", "d.down.total=", "d.up.rate=", "d.up.total="}

    go func() {
        defer close(ch)
        err := collector.parseDownloadDetailsMetrics(a, cmds, ch)
        assert.Nil(t, err)
    }()

    for range ch {
        // Consume the channel
    }
}

func TestDownloadsCollector_gatherDownloadDetailLabels(t *testing.T) {
    collector := NewDownloadsCollector(nil, CollectorOpts{})
    torSlice := []any{"hash1", "name1"}

    labels, err := collector.gatherDownloadDetailLabels(torSlice)
    assert.Nil(t, err)
    assert.Equal(t, []string{"hash1", "name1"}, labels)
}

func TestDownloadsCollector_getDownloadDetailCommands(t *testing.T) {
    collector := NewDownloadsCollector(nil, CollectorOpts{})
    cmds := collector.getDownloadDetailCommands()
    assert.Equal(t, defaultActiveCommands, cmds)
}

func TestDownloadsCollector_Describe(t *testing.T) {
    collector := NewDownloadsCollector(nil, CollectorOpts{DownloadDetails: true})
    ch := make(chan *prometheus.Desc)

    go func() {
        defer close(ch)
        collector.Describe(ch)
    }()

    for range ch {
        // Consume the channel
    }
}
