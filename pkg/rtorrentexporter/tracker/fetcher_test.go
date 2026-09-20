package tracker

import (
	"context"
	"sync"
	"testing"

	"github.com/aauren/rtorrent/rtorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	hash       = "testhash"
	testField1 = "field1"
	testField2 = "field2"
)

// MockSource is a mock implementation of the Source interface.
type MockSource struct {
	mock.Mock
}

func (m *MockSource) TrackerWithDetails(ctx context.Context, ti *rtorrent.TrackerIndex, fields []rtorrent.TrackerField) ([]*rtorrent.Tracker, error) {
	args := m.Called(ctx, ti, fields)
	return args.Get(0).([]*rtorrent.Tracker), args.Error(1)
}

func TestFetcher_GetTrackersByHashIndexAllFields(t *testing.T) {
	t.Parallel()
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := t.Context()
	index := 1
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, mock.Anything, rtorrent.AllTrackerFields()).Return(expectedTrackers, nil)

	trackers, err := fetcher.GetTrackersByHashIndexAllFields(ctx, hash, index)
	require.NoError(t, err)
	assert.Equal(t, expectedTrackers, trackers)

	mockSource.AssertExpectations(t)
}

func TestFetcher_GetTrackersByHashAllFields(t *testing.T) {
	t.Parallel()
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := t.Context()
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, mock.Anything, rtorrent.AllTrackerFields()).Return(expectedTrackers, nil)

	trackers, err := fetcher.GetTrackersByHashAllFields(ctx, hash)
	require.NoError(t, err)
	assert.Equal(t, expectedTrackers, trackers)

	mockSource.AssertExpectations(t)
}

func TestFetcher_GetTrackersAllFields(t *testing.T) {
	t.Parallel()
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := t.Context()
	ti := &rtorrent.TrackerIndex{}
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, ti, rtorrent.AllTrackerFields()).Return(expectedTrackers, nil)

	trackers, err := fetcher.GetTrackersAllFields(ctx, ti)
	require.NoError(t, err)
	assert.Equal(t, expectedTrackers, trackers)

	mockSource.AssertExpectations(t)
}

func TestFetcher_GetTrackersByHashIndexSelectedFields(t *testing.T) {
	t.Parallel()
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := t.Context()
	index := 1
	fields := []rtorrent.TrackerField{testField1, testField2}
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, mock.Anything, fields).Return(expectedTrackers, nil)

	trackers, err := fetcher.GetTrackersByHashIndexSelectedFields(ctx, hash, index, fields)
	require.NoError(t, err)
	assert.Equal(t, expectedTrackers, trackers)

	mockSource.AssertExpectations(t)
}

func TestFetcher_GetTrackersByHashSelectedFields(t *testing.T) {
	t.Parallel()
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := t.Context()
	fields := []rtorrent.TrackerField{testField1, testField2}
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, mock.Anything, fields).Return(expectedTrackers, nil)

	trackers, err := fetcher.GetTrackersByHashSelectedFields(ctx, hash, fields)
	require.NoError(t, err)
	assert.Equal(t, expectedTrackers, trackers)

	mockSource.AssertExpectations(t)
}

func TestFetcher_GetTrackersSelectedFields(t *testing.T) {
	t.Parallel()
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := t.Context()
	ti := &rtorrent.TrackerIndex{}
	fields := []rtorrent.TrackerField{testField1, testField2}
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, ti, fields).Return(expectedTrackers, nil)

	trackers, err := fetcher.GetTrackersSelectedFields(ctx, ti, fields)
	require.NoError(t, err)
	assert.Equal(t, expectedTrackers, trackers)

	mockSource.AssertExpectations(t)
}

func TestFetcher_Run(t *testing.T) {
	t.Parallel()
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wg := &sync.WaitGroup{}
	inCH := make(chan *FetchRequest)
	outCH := make(chan *TrackerResponse)

	wg.Go(func() {
		fetcher.Run(ctx, inCH, outCH)
	})

	// These share one fetcher and one pair of channels, so they have to run in order rather than as parallel subtests
	ti := &rtorrent.TrackerIndex{}
	expectedTrackers := []*rtorrent.Tracker{{}}
	fields := []rtorrent.TrackerField{testField1, testField2}
	mockSource.On("TrackerWithDetails", ctx, ti, rtorrent.AllTrackerFields()).Return(expectedTrackers, nil)
	mockSource.On("TrackerWithDetails", ctx, ti, fields).Return(expectedTrackers, nil)

	// Fetch all fields
	inCH <- &FetchRequest{TrackerIndex: ti}
	resp := <-outCH
	require.NotNil(t, resp)
	require.NoError(t, resp.Error)
	assert.Equal(t, expectedTrackers, resp.Trackers)
	assert.Equal(t, ti, resp.TrackerIndex)

	// Fetch selected fields
	inCH <- &FetchRequest{TrackerIndex: ti, Fields: fields}
	resp = <-outCH
	require.NotNil(t, resp)
	require.NoError(t, resp.Error)
	assert.Equal(t, expectedTrackers, resp.Trackers)
	mockSource.AssertExpectations(t)

	// Context done stops the fetcher
	cancel()
	wg.Wait()
}
