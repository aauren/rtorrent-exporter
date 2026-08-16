package tracker

import (
	"context"
	"sync"
	"testing"

	"github.com/aauren/rtorrent/rtorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := context.Background()
	index := 1
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, mock.Anything, rtorrent.AllTrackerFields()).Return(expectedTrackers, nil)

	t.Run("success", func(t *testing.T) {
		trackers, err := fetcher.GetTrackersByHashIndexAllFields(ctx, hash, index)
		assert.NoError(t, err)
		assert.Equal(t, expectedTrackers, trackers)
	})

	mockSource.AssertExpectations(t)
}

func TestFetcher_GetTrackersByHashAllFields(t *testing.T) {
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := context.Background()
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, mock.Anything, rtorrent.AllTrackerFields()).Return(expectedTrackers, nil)

	t.Run("success", func(t *testing.T) {
		trackers, err := fetcher.GetTrackersByHashAllFields(ctx, hash)
		assert.NoError(t, err)
		assert.Equal(t, expectedTrackers, trackers)
	})

	mockSource.AssertExpectations(t)
}

func TestFetcher_GetTrackersAllFields(t *testing.T) {
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := context.Background()
	ti := &rtorrent.TrackerIndex{}
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, ti, rtorrent.AllTrackerFields()).Return(expectedTrackers, nil)

	t.Run("success", func(t *testing.T) {
		trackers, err := fetcher.GetTrackersAllFields(ctx, ti)
		assert.NoError(t, err)
		assert.Equal(t, expectedTrackers, trackers)
	})

	mockSource.AssertExpectations(t)
}

func TestFetcher_GetTrackersByHashIndexSelectedFields(t *testing.T) {
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := context.Background()
	index := 1
	fields := []rtorrent.TrackerField{testField1, testField2}
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, mock.Anything, fields).Return(expectedTrackers, nil)

	t.Run("success", func(t *testing.T) {
		trackers, err := fetcher.GetTrackersByHashIndexSelectedFields(ctx, hash, index, fields)
		assert.NoError(t, err)
		assert.Equal(t, expectedTrackers, trackers)
	})

	mockSource.AssertExpectations(t)
}

func TestFetcher_GetTrackersByHashSelectedFields(t *testing.T) {
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := context.Background()
	fields := []rtorrent.TrackerField{testField1, testField2}
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, mock.Anything, fields).Return(expectedTrackers, nil)

	t.Run("success", func(t *testing.T) {
		trackers, err := fetcher.GetTrackersByHashSelectedFields(ctx, hash, fields)
		assert.NoError(t, err)
		assert.Equal(t, expectedTrackers, trackers)
	})

	mockSource.AssertExpectations(t)
}

func TestFetcher_GetTrackersSelectedFields(t *testing.T) {
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx := context.Background()
	ti := &rtorrent.TrackerIndex{}
	fields := []rtorrent.TrackerField{testField1, testField2}
	expectedTrackers := []*rtorrent.Tracker{{}}

	mockSource.On("TrackerWithDetails", ctx, ti, fields).Return(expectedTrackers, nil)

	t.Run("success", func(t *testing.T) {
		trackers, err := fetcher.GetTrackersSelectedFields(ctx, ti, fields)
		assert.NoError(t, err)
		assert.Equal(t, expectedTrackers, trackers)
	})

	mockSource.AssertExpectations(t)
}

func TestFetcher_Run(t *testing.T) {
	mockSource := new(MockSource)
	fetcher := NewFetcher(mockSource)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}
	inCH := make(chan *FetchRequest)
	outCH := make(chan *TrackerResponse)

	wg.Go(func() {
		fetcher.Run(ctx, inCH, outCH)
	})

	t.Run("fetch all fields", func(t *testing.T) {
		ti := &rtorrent.TrackerIndex{}
		expectedTrackers := []*rtorrent.Tracker{{}}
		mockSource.On("TrackerWithDetails", ctx, ti, rtorrent.AllTrackerFields()).Return(expectedTrackers, nil)

		inCH <- &FetchRequest{TrackerIndex: ti}
		resp := <-outCH

		assert.NoError(t, resp.Error)
		assert.Equal(t, expectedTrackers, resp.Trackers)
		mockSource.AssertExpectations(t)
	})

	t.Run("fetch selected fields", func(t *testing.T) {
		ti := &rtorrent.TrackerIndex{}
		fields := []rtorrent.TrackerField{testField1, testField2}
		expectedTrackers := []*rtorrent.Tracker{{}}
		mockSource.On("TrackerWithDetails", ctx, ti, fields).Return(expectedTrackers, nil)

		inCH <- &FetchRequest{TrackerIndex: ti, Fields: fields}
		resp := <-outCH

		assert.NoError(t, resp.Error)
		assert.Equal(t, expectedTrackers, resp.Trackers)
		mockSource.AssertExpectations(t)
	})

	t.Run("context done", func(t *testing.T) {
		cancel()
		wg.Wait()
	})
}
