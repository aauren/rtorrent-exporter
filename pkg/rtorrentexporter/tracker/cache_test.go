package tracker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/aauren/rtorrent/rtorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		mt := &ModifiedTracker{Tracker: tracker}
		ttci := &TimedTrackerCacheInstance{
			Tracker:   mt,
			FetchedAt: time.Now(),
		}
		c.trackerCache[ti] = ttci

		result, ok, err := c.getTrackerFromCacheOnly(&ti)
		require.NoError(t, err)
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
		require.ErrorIs(t, err, rtorrent.ErrBadData)
		assert.False(t, ok)
		assert.Nil(t, result)
	})

	t.Run("tracker not found in cache, no error", func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}

		result, ok, err := c.getTrackerFromCacheOnly(&ti)
		require.NoError(t, err)
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
		mt := &ModifiedTracker{Tracker: tracker}
		ttci := &TimedTrackerCacheInstance{
			Tracker:   mt,
			FetchedAt: time.Now(),
		}
		c.trackerCache[ti] = ttci

		result := c.GetTrackerFromCacheNonBlocking(&ti)
		assert.Equal(t, mt, result)

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
		mt := &ModifiedTracker{Tracker: tracker}
		ttci := &TimedTrackerCacheInstance{
			Tracker:   mt,
			FetchedAt: time.Now().Add(-15 * time.Minute),
		}
		c.trackerCache[ti] = ttci

		result := c.GetTrackerFromCacheNonBlocking(&ti)
		assert.Equal(t, mt, result)

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
			blockingReqCtx: t.Context(),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		tracker := &rtorrent.Tracker{}
		tracker = tracker.CloneWithTrackerIndex(&ti)
		mt := &ModifiedTracker{Tracker: tracker}
		ttci := &TimedTrackerCacheInstance{
			Tracker:   mt,
			FetchedAt: time.Now(),
		}
		c.trackerCache[ti] = ttci

		result, err := c.GetTrackerFromCacheBlocking(t.Context(), &ti)
		require.NoError(t, err)
		assert.Equal(t, mt, result)
	})

	// This one runs inside a synctest bubble, so the sleep below and the cacher's own poll interval both run against a fake clock. The
	// test finishes as fast as the scheduler allows and can't flake on a loaded machine.
	t.Run("tracker not found in cache", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := &Cacher{
				trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
				trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
				cacheCheckChan:    make(chan *FetchRequest, 1),
				reqChan:           make(chan *FetchRequest, 1),
				cOpts: CacheOpts{
					MaxAge: 10 * time.Minute,
					MinAge: 5 * time.Minute,
				},
				blockingReqCtx: t.Context(),
			}

			ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}

			go func() {
				time.Sleep(100 * time.Millisecond)
				tracker := &rtorrent.Tracker{}
				tracker = tracker.CloneWithTrackerIndex(&ti)
				// We go through cacheTrackers rather than writing the map directly, because that's how a fetcher response lands in
				// the cache, and it's what puts the write under cacheMu where the blocked reader can safely see it
				c.cacheTrackers(&TrackerResponse{
					Trackers:  []*rtorrent.Tracker{tracker},
					FetchedAt: time.Now(),
				})
			}()

			result, err := c.GetTrackerFromCacheBlocking(t.Context(), &ti)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, &ti, result.Tracker.TrackerIndex())
		})
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
			blockingReqCtx: t.Context(),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		result, err := c.GetTrackerFromCacheBlocking(ctx, &ti)
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, result)
	})

	t.Run("tracker not found in cache, blockingCancelCtx canceled", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
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
			c.blockingReqCtx, c.blockingReqCancel = context.WithCancel(t.Context())

			go func() {
				time.Sleep(100 * time.Millisecond)
				c.blockingReqCancel()
			}()

			result, err := c.GetTrackerFromCacheBlocking(t.Context(), &ti)
			require.ErrorIs(t, err, ErrBlockingRequestCancelled)
			assert.Nil(t, result)
		})
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
		mt := &ModifiedTracker{
			Tracker:           tracker,
			SubstitutedDomain: unknownDomain,
		}
		tr := &TrackerResponse{
			Trackers:  []*rtorrent.Tracker{tracker},
			FetchedAt: time.Now(),
		}

		c.cacheTrackers(tr)

		require.Contains(t, c.trackerCache, *ti)
		assert.Equal(t, mt, c.trackerCache[*ti].Tracker)
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
		mt1 := &ModifiedTracker{
			Tracker:           tracker1,
			SubstitutedDomain: unknownDomain,
		}
		tracker2 := &rtorrent.Tracker{}
		tracker2 = tracker2.CloneWithTrackerIndex(ti2)
		mt2 := &ModifiedTracker{
			Tracker:           tracker2,
			SubstitutedDomain: unknownDomain,
		}
		ti1HashOnly := rtorrent.NewTrackerNoIndex(ti1.InfoHash)
		tracker1HashOnly := tracker1.CloneWithTrackerIndex(ti1HashOnly)
		mt1HashOnly := &ModifiedTracker{
			Tracker:           tracker1HashOnly,
			SubstitutedDomain: unknownDomain,
		}
		tr := &TrackerResponse{
			Trackers:  []*rtorrent.Tracker{tracker1, tracker2},
			FetchedAt: time.Now(),
		}

		c.cacheTrackers(tr)

		require.Contains(t, c.trackerCache, *ti1)
		require.Contains(t, c.trackerCache, *ti2)
		require.Contains(t, c.trackerCache, *ti1HashOnly)
		assert.Equal(t, mt1, c.trackerCache[*ti1].Tracker)
		assert.Equal(t, mt2, c.trackerCache[*ti2].Tracker)
		assert.Equal(t, mt1HashOnly, c.trackerCache[*ti1HashOnly].Tracker)
		assert.Empty(t, c.trackerRespErrors)
	})
}

func TestCacheTrackersError(t *testing.T) {
	t.Run("nil tracker", func(t *testing.T) {
		ctx, cancelFunc := context.WithCancel(t.Context())
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
			blockingReqCtx:    ctx,
			blockingReqCancel: cancelFunc,
		}

		err := rtorrent.ErrNilTrackerIndex
		tr := &TrackerResponse{Error: err}

		c.cacheTrackersError(tr)

		require.ErrorIs(t, ctx.Err(), context.Canceled, "expected that context we passed in would be canceled on a nil tracker")
		require.NotNil(t, c.blockingReqCtx, "expected that a new context would be created for blockingReqCtx")
		require.NoError(t, c.blockingReqCtx.Err(), "expected that the new blockingReqCtx would not already be canceled")
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
	// The bubble is what lets us drop the one second timeout guard that used to wrap the channel read. If the stale check ever stops
	// sending, every goroutine here blocks forever and synctest reports the deadlock instead of the test waiting out a timer.
	synctest.Test(t, func(t *testing.T) {
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
		mt := &ModifiedTracker{Tracker: tracker}
		ttci := &TimedTrackerCacheInstance{
			Tracker:   mt,
			FetchedAt: time.Now().Add(-15 * time.Minute),
		}
		c.trackerCache[ti] = ttci

		wg := &sync.WaitGroup{}
		wg.Go(func() {
			c.checkCacheForStaleItems(t.Context())
		})

		fr := <-c.reqChan
		require.NotNil(t, fr)
		assert.Equal(t, &ti, fr.TrackerIndex)

		wg.Wait()
	})
}
