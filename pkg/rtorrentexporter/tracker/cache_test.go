package tracker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aauren/rtorrent/rtorrent"
	"github.com/stretchr/testify/assert"
)

func TestTimedTrackerCacheInstance_IsStale(t *testing.T) {
	tests := []struct {
		name      string
		fetchedAt time.Time
		minAge    time.Duration
		maxAge    time.Duration
		expected  bool
	}{
		{"not stale, within minAge", time.Now().Add(-1 * time.Minute), 5 * time.Minute, 10 * time.Minute, false},
		{"stale, beyond maxAge", time.Now().Add(-15 * time.Minute), 5 * time.Minute, 10 * time.Minute, true},
		{"stale, exactly at maxAge", time.Now().Add(-10 * time.Minute), 5 * time.Minute, 10 * time.Minute, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ttci := &TimedTrackerCacheInstance{
				FetchedAt: tt.fetchedAt,
			}
			result := ttci.IsStale(tt.minAge, tt.maxAge)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetTrackerFromCacheOnly(t *testing.T) {
	t.Run("tracker found in cache", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		tracker := &rtorrent.Tracker{}
		tracker = tracker.CloneWithTrackerIndex(&ti)
		ttci := &TimedTrackerCacheInstance{
			Tracker:   tracker,
			FetchedAt: time.Now(),
		}
		c.trackerCache[ti] = ttci

		result, ok, err := c.getTrackerFromCacheOnly(&ti)
		assert.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, ttci, result)
	})

	t.Run("tracker not found in cache, error found", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		c.trackerRespErrors[ti] = rtorrent.ErrBadData

		result, ok, err := c.getTrackerFromCacheOnly(&ti)
		assert.Error(t, err)
		assert.False(t, ok)
		assert.Nil(t, result)
		assert.Equal(t, rtorrent.ErrBadData, err)
	})

	t.Run("tracker not found in cache, no error", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}

		result, ok, err := c.getTrackerFromCacheOnly(&ti)
		assert.NoError(t, err)
		assert.False(t, ok)
		assert.Nil(t, result)
	})
}

func TestGetTrackerFromCacheNonBlocking(t *testing.T) {
	t.Run("tracker found in cache and not stale", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
			cacheCheckChan:    make(chan *FetchRequest, 1),
			reqChan:           make(chan *FetchRequest, 1),
			cOpts: CacheOpts{
				MaxAge: 10 * time.Minute,
				MinAge: 5 * time.Minute,
			},
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		tracker := &rtorrent.Tracker{}
		tracker = tracker.CloneWithTrackerIndex(&ti)
		ttci := &TimedTrackerCacheInstance{
			Tracker:   tracker,
			FetchedAt: time.Now(),
		}
		c.trackerCache[ti] = ttci

		result := c.GetTrackerFromCacheNonBlocking(&ti)
		assert.Equal(t, tracker, result)

		foundNoReq := false
		select {
		case <-c.reqChan:
			t.Fatal("expected fetch request, but got none")
		default:
			foundNoReq = true
		}
		assert.True(t, foundNoReq, "no request should have been found in the reqChan for a positive cache hit")
	})

	t.Run("tracker found in cache but stale", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
			cacheCheckChan:    make(chan *FetchRequest, 1),
			reqChan:           make(chan *FetchRequest, 1),
			cOpts: CacheOpts{
				MaxAge: 10 * time.Minute,
				MinAge: 5 * time.Minute,
			},
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		tracker := &rtorrent.Tracker{}
		tracker = tracker.CloneWithTrackerIndex(&ti)
		ttci := &TimedTrackerCacheInstance{
			Tracker:   tracker,
			FetchedAt: time.Now().Add(-15 * time.Minute),
		}
		c.trackerCache[ti] = ttci

		result := c.GetTrackerFromCacheNonBlocking(&ti)
		assert.Equal(t, tracker, result)

		select {
		case fr := <-c.reqChan:
			assert.Equal(t, &ti, fr.TrackerIndex)
		default:
			t.Fatal("expected fetch request, but got none")
		}
	})

	t.Run("tracker not found in cache", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
			cacheCheckChan:    make(chan *FetchRequest, 1),
			reqChan:           make(chan *FetchRequest, 1),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}

		result := c.GetTrackerFromCacheNonBlocking(&ti)
		assert.Nil(t, result)

		select {
		case fr := <-c.cacheCheckChan:
			assert.Equal(t, &ti, fr.TrackerIndex)
		default:
			t.Fatal("expected cache check request, but got none")
		}
	})
}

func TestGetTrackerFromCacheBlocking(t *testing.T) {
	t.Run("tracker found in cache", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
			cacheCheckChan:    make(chan *FetchRequest, 1),
			reqChan:           make(chan *FetchRequest, 1),
			cOpts: CacheOpts{
				MaxAge: 10 * time.Minute,
				MinAge: 5 * time.Minute,
			},
			blockingReqCtx: context.Background(),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		tracker := &rtorrent.Tracker{}
		tracker = tracker.CloneWithTrackerIndex(&ti)
		ttci := &TimedTrackerCacheInstance{
			Tracker:   tracker,
			FetchedAt: time.Now(),
		}
		c.trackerCache[ti] = ttci

		result, err := c.GetTrackerFromCacheBlocking(context.Background(), &ti)
		assert.NoError(t, err)
		assert.Equal(t, tracker, result)
	})

	t.Run("tracker not found in cache", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
			cacheCheckChan:    make(chan *FetchRequest, 1),
			reqChan:           make(chan *FetchRequest, 1),
			cOpts: CacheOpts{
				MaxAge: 10 * time.Minute,
				MinAge: 5 * time.Minute,
			},
			blockingReqCtx: context.Background(),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}

		go func() {
			time.Sleep(100 * time.Millisecond)
			tracker := &rtorrent.Tracker{}
			tracker = tracker.CloneWithTrackerIndex(&ti)
			ttci := &TimedTrackerCacheInstance{
				Tracker:   tracker,
				FetchedAt: time.Now(),
			}
			c.trackerCache[ti] = ttci
		}()

		result, err := c.GetTrackerFromCacheBlocking(context.Background(), &ti)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, &ti, result.TrackerIndex())
	})

	t.Run("tracker not found in cache, context canceled", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
			cacheCheckChan:    make(chan *FetchRequest, 1),
			reqChan:           make(chan *FetchRequest, 1),
			cOpts: CacheOpts{
				MaxAge: 10 * time.Minute,
				MinAge: 5 * time.Minute,
			},
			blockingReqCtx: context.Background(),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result, err := c.GetTrackerFromCacheBlocking(ctx, &ti)
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("tracker not found in cache, blockingCancelCtx canceled", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
			cacheCheckChan:    make(chan *FetchRequest, 1),
			reqChan:           make(chan *FetchRequest, 1),
			cOpts: CacheOpts{
				MaxAge: 10 * time.Minute,
				MinAge: 5 * time.Minute,
			},
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}

		// Create a context with cancel function for blockingReqCtx
		c.blockingReqCtx, c.blockingReqCancel = context.WithCancel(context.Background())

		go func() {
			time.Sleep(100 * time.Millisecond)
			c.blockingReqCancel()
		}()

		result, err := c.GetTrackerFromCacheBlocking(context.Background(), &ti)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

func TestCacheTrackers(t *testing.T) {
	t.Run("single tracker", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
		}

		ti := &rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		tracker := &rtorrent.Tracker{}
		tracker = tracker.CloneWithTrackerIndex(ti)
		tr := &TrackerResponse{
			Trackers:  []*rtorrent.Tracker{tracker},
			FetchedAt: time.Now(),
		}

		c.cacheTrackers(tr)

		assert.Equal(t, tracker, c.trackerCache[*ti].Tracker)
		assert.Empty(t, c.trackerRespErrors)
	})

	t.Run("multiple trackers", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
		}

		ti1 := &rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		ti2 := &rtorrent.TrackerIndex{InfoHash: "12345", Index: 2}
		tracker1 := &rtorrent.Tracker{}
		tracker1 = tracker1.CloneWithTrackerIndex(ti1)
		tracker2 := &rtorrent.Tracker{}
		tracker2 = tracker2.CloneWithTrackerIndex(ti2)
		ti1HashOnly := rtorrent.NewTrackerNoIndex(ti1.InfoHash)
		tracker1HashOnly := tracker1.CloneWithTrackerIndex(ti1HashOnly)
		tr := &TrackerResponse{
			Trackers:  []*rtorrent.Tracker{tracker1, tracker2},
			FetchedAt: time.Now(),
		}

		c.cacheTrackers(tr)

		assert.Equal(t, tracker1, c.trackerCache[*ti1].Tracker)
		assert.Equal(t, tracker2, c.trackerCache[*ti2].Tracker)
		assert.Equal(t, tracker1HashOnly, c.trackerCache[*ti1HashOnly].Tracker)
		assert.Empty(t, c.trackerRespErrors)
	})
}

func TestCacheTrackersError(t *testing.T) {
	t.Run("nil tracker", func(t *testing.T) {
		ctx, cancelFunc := context.WithCancel(context.Background())
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
			blockingReqCtx:    ctx,
			blockingReqCancel: cancelFunc,
		}

		err := rtorrent.ErrNilTrackerIndex
		tr := &TrackerResponse{Error: err}

		c.cacheTrackersError(tr)

		assert.Equal(t, context.Canceled, ctx.Err(), "expected that context we passed in would be canceled on a nil trakcer")
		assert.Nil(t, c.blockingReqCtx.Err(), "expected that a new context would be created for blockingReqCtx")
		assert.Empty(t, c.trackerRespErrors, "expected that a nil context would not write any errors to the trackerRespErrors map")
	})

	t.Run("multiple trackers", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
		}

		ti1 := &rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		ti2 := &rtorrent.TrackerIndex{InfoHash: "12345", Index: 2}
		tracker1 := &rtorrent.Tracker{}
		tracker1 = tracker1.CloneWithTrackerIndex(ti1)
		tracker2 := &rtorrent.Tracker{}
		tracker2 = tracker2.CloneWithTrackerIndex(ti2)
		err := errors.New("test error")
		tr := &TrackerResponse{
			Trackers: []*rtorrent.Tracker{tracker1, tracker2},
			Error:    err,
		}

		c.cacheTrackersError(tr)

		assert.Equal(t, err, c.trackerRespErrors[*ti1])
		assert.Equal(t, err, c.trackerRespErrors[*ti2])
		assert.Empty(t, c.trackerCache)
	})
}

func TestCheckCacheForStaleItems(t *testing.T) {
	c := &Cacher{
		trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
		trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
		reqChan:           make(chan *FetchRequest, 1),
		cOpts: CacheOpts{
			MaxAge: 10 * time.Minute,
			MinAge: 5 * time.Minute,
		},
	}

	ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
	tracker := &rtorrent.Tracker{}
	tracker = tracker.CloneWithTrackerIndex(&ti)
	ttci := &TimedTrackerCacheInstance{
		Tracker:   tracker,
		FetchedAt: time.Now().Add(-15 * time.Minute),
	}
	c.trackerCache[ti] = ttci

	wg := &sync.WaitGroup{}
	wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go c.checkCacheForStaleItems(ctx, wg)

	select {
	case fr := <-c.reqChan:
		assert.Equal(t, &ti, fr.TrackerIndex)
	case <-time.After(1 * time.Second):
		t.Fatal("expected fetch request, but got none")
	}

	wg.Wait()
}
