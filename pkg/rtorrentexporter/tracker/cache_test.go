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
	t.Parallel()
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
			t.Parallel()
			ttci := &TimedTrackerCacheInstance{
				FetchedAt: tt.fetchedAt,
			}
			result := ttci.IsStale(tt.minAge, tt.maxAge)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetTrackerFromCacheOnly(t *testing.T) {
	t.Parallel()
	t.Run("tracker found in cache", func(t *testing.T) {
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
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

		result, tte := c.getTrackerFromCacheOnly(&ti)
		assert.Nil(t, tte)
		assert.Equal(t, ttci, result)
	})

	t.Run("tracker not found in cache, error found", func(t *testing.T) {
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		c.trackerRespErrors[ti] = &TimedTrackerError{Err: rtorrent.ErrBadData, FetchedAt: time.Now()}

		result, tte := c.getTrackerFromCacheOnly(&ti)
		require.NotNil(t, tte)
		require.ErrorIs(t, tte.Err, rtorrent.ErrBadData)
		assert.Nil(t, result)
	})

	t.Run("tracker not found in cache, no error", func(t *testing.T) {
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}

		result, tte := c.getTrackerFromCacheOnly(&ti)
		assert.Nil(t, tte)
		assert.Nil(t, result)
	})
}

func TestGetTrackerFromCacheNonBlocking(t *testing.T) {
	t.Parallel()
	t.Run("tracker found in cache and not stale", func(t *testing.T) {
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
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
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
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
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
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

	// A cached error is a negative cache hit, so we shouldn't be asking rtorrent again on every scrape
	t.Run("tracker has cached error", func(t *testing.T) {
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
			cacheCheckChan:    make(chan *FetchRequest, 1),
			reqChan:           make(chan *FetchRequest, 1),
			cOpts: CacheOpts{
				MaxAge: 10 * time.Minute,
				MinAge: 5 * time.Minute,
			},
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		c.trackerRespErrors[ti] = &TimedTrackerError{Err: rtorrent.ErrBadData, FetchedAt: time.Now()}

		result := c.GetTrackerFromCacheNonBlocking(&ti)
		assert.Nil(t, result)

		select {
		case <-c.cacheCheckChan:
			t.Fatal("expected no cache check request for a cached error, but got one")
		case <-c.reqChan:
			t.Fatal("expected no fetch request for a cached error, but got one")
		default:
		}
	})

	t.Run("tracker has stale cached error", func(t *testing.T) {
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
			cacheCheckChan:    make(chan *FetchRequest, 1),
			reqChan:           make(chan *FetchRequest, 1),
			cOpts: CacheOpts{
				MaxAge: 10 * time.Minute,
				MinAge: 5 * time.Minute,
			},
		}

		ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		c.trackerRespErrors[ti] = &TimedTrackerError{Err: rtorrent.ErrBadData, FetchedAt: time.Now().Add(-15 * time.Minute)}

		result := c.GetTrackerFromCacheNonBlocking(&ti)
		assert.Nil(t, result)

		select {
		case fr := <-c.reqChan:
			assert.Equal(t, &ti, fr.TrackerIndex)
		default:
			t.Fatal("expected fetch request for a stale cached error, but got none")
		}
	})
}

// A scraper parked on a full cacheCheckChan refills the buffer the instant checkCacheCheckChan reads from it, so the old re-queue
// send had nowhere to go and blocked the cacher's main loop. In the bubble that shows up as a deadlock instead of a hung test.
func TestCheckCacheCheckChan_doesNotBlockOnRequeue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
			cacheCheckChan:    make(chan *FetchRequest, 1),
			reqChan:           make(chan *FetchRequest, 1),
			cOpts: CacheOpts{
				MaxAge: 10 * time.Minute,
				MinAge: 5 * time.Minute,
			},
		}

		fr1 := &FetchRequest{TrackerIndex: rtorrent.NewTrackerNoIndex("11111")}
		fr2 := &FetchRequest{TrackerIndex: rtorrent.NewTrackerNoIndex("22222")}
		c.reqChan <- &FetchRequest{TrackerIndex: rtorrent.NewTrackerNoIndex("00000")}
		c.cacheCheckChan <- fr1
		go func() { c.cacheCheckChan <- fr2 }()
		synctest.Wait()

		c.checkCacheCheckChan()

		// fr1 had nowhere to go and was dropped, fr2 is still waiting its turn
		require.Len(t, c.cacheCheckChan, 1)
		assert.Equal(t, fr2, <-c.cacheCheckChan)
	})
}

func TestCheckCacheCheckChan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		errAge      time.Duration
		wantForward bool
	}{
		{"cached error is not forwarded", 0, false},
		{"stale cached error is forwarded", 15 * time.Minute, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &Cacher{
				trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
				trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
				cacheCheckChan:    make(chan *FetchRequest, 1),
				reqChan:           make(chan *FetchRequest, 1),
				cOpts: CacheOpts{
					MaxAge: 10 * time.Minute,
					MinAge: 5 * time.Minute,
				},
			}

			ti := rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
			c.trackerRespErrors[ti] = &TimedTrackerError{Err: rtorrent.ErrBadData, FetchedAt: time.Now().Add(-tt.errAge)}
			c.cacheCheckChan <- &FetchRequest{TrackerIndex: &ti}

			c.checkCacheCheckChan()

			select {
			case fr := <-c.reqChan:
				require.True(t, tt.wantForward, "expected no fetch request for a cached error, but got one")
				assert.Equal(t, &ti, fr.TrackerIndex)
			default:
				require.False(t, tt.wantForward, "expected fetch request for a stale cached error, but got none")
			}
		})
	}
}

func TestGetTrackerFromCacheBlocking(t *testing.T) {
	t.Parallel()
	t.Run("tracker found in cache", func(t *testing.T) {
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
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
				trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
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
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
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
				trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
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
	t.Parallel()
	t.Run("single tracker", func(t *testing.T) {
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
		}

		ti := &rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		tracker := &rtorrent.Tracker{}
		tracker = tracker.CloneWithTrackerIndex(ti)
		mt := &ModifiedTracker{
			Tracker:           tracker,
			SubstitutedDomain: UnknownDomain,
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
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
		}

		ti1 := &rtorrent.TrackerIndex{InfoHash: "12345", Index: 1}
		ti2 := &rtorrent.TrackerIndex{InfoHash: "12345", Index: 2}
		tracker1 := &rtorrent.Tracker{}
		tracker1 = tracker1.CloneWithTrackerIndex(ti1)
		mt1 := &ModifiedTracker{
			Tracker:           tracker1,
			SubstitutedDomain: UnknownDomain,
		}
		tracker2 := &rtorrent.Tracker{}
		tracker2 = tracker2.CloneWithTrackerIndex(ti2)
		mt2 := &ModifiedTracker{
			Tracker:           tracker2,
			SubstitutedDomain: UnknownDomain,
		}
		ti1HashOnly := rtorrent.NewTrackerNoIndex(ti1.InfoHash)
		tracker1HashOnly := tracker1.CloneWithTrackerIndex(ti1HashOnly)
		mt1HashOnly := &ModifiedTracker{
			Tracker:           tracker1HashOnly,
			SubstitutedDomain: UnknownDomain,
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

	// A torrent with no trackers at all comes back as an empty success, which must not panic and must leave something in the cache so
	// that callers stop re-requesting it
	t.Run("no trackers", func(t *testing.T) {
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
		}

		ti := rtorrent.NewTrackerNoIndex("12345")
		tr := &TrackerResponse{
			TrackerIndex: ti,
			FetchedAt:    time.Now(),
		}

		require.NotPanics(t, func() { c.cacheTrackers(tr) })

		require.Contains(t, c.trackerCache, *ti)
		assert.Equal(t, UnknownDomain, c.trackerCache[*ti].Tracker.SubstitutedDomain)
		assert.Empty(t, c.trackerRespErrors)
	})
}

func TestCacheTrackersError(t *testing.T) {
	t.Parallel()
	t.Run("nil tracker", func(t *testing.T) {
		t.Parallel()
		ctx, cancelFunc := context.WithCancel(t.Context())
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
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
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
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

		assert.Equal(t, err, c.trackerRespErrors[*ti1].Err)
		assert.Equal(t, err, c.trackerRespErrors[*ti2].Err)
		assert.Empty(t, c.trackerCache)
	})

	// This is what every transport level failure looks like, an error with nil trackers, so it must not panic and the error has to be
	// keyed on the index that was asked for since there's nothing else to key it on
	t.Run("error with no trackers", func(t *testing.T) {
		t.Parallel()
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
		}

		ti := rtorrent.NewTrackerNoIndex("12345")
		err := errors.New("test error")
		tr := &TrackerResponse{
			TrackerIndex: ti,
			Error:        err,
		}

		require.NotPanics(t, func() { c.cacheTrackersError(tr) })

		assert.Equal(t, err, c.trackerRespErrors[*ti].Err)
		assert.Empty(t, c.trackerCache)
	})
}

func TestCheckCacheForStaleItems(t *testing.T) {
	// The bubble is what lets us drop the one second timeout guard that used to wrap the channel read. If the stale check ever stops
	// sending, every goroutine here blocks forever and synctest reports the deadlock instead of the test waiting out a timer.
	synctest.Test(t, func(t *testing.T) {
		c := &Cacher{
			trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
			trackerRespErrors: make(map[rtorrent.TrackerIndex]*TimedTrackerError),
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

func TestCheckCacheForStaleItems_errorExpiry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		age        time.Duration
		wantCached bool
	}{
		{name: "fresh error", wantCached: true},
		{name: "error at max age", age: time.Hour},
		{name: "expired error", age: 2 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewCacher(nil, CacheOpts{MinAge: time.Minute, MaxAge: time.Hour, MaxParallelRequests: 1})
			ti := rtorrent.NewTrackerNoIndex("12345")
			c.cacheTrackersError(&TrackerResponse{
				TrackerIndex: ti,
				Error:        assert.AnError,
				FetchedAt:    time.Now().Add(-tt.age),
			})

			for range 2 {
				c.checkCacheForStaleItems(t.Context())
				assert.Empty(t, c.reqChan, "errors shouldn't trigger background fetches")
				_, cached := c.trackerRespErrors[*ti]
				assert.Equal(t, tt.wantCached, cached)
			}
			if tt.wantCached {
				return
			}

			// We still fetch an expired entry when a scrape asks for it again
			assert.Nil(t, c.GetTrackerFromCacheNonBlocking(ti))
			c.checkCacheCheckChan()
			require.Len(t, c.reqChan, 1)
			assert.Equal(t, ti, (<-c.reqChan).TrackerIndex)
			c.cacheTrackers(&TrackerResponse{
				TrackerIndex: ti,
				Trackers:     []*rtorrent.Tracker{new(rtorrent.Tracker).CloneWithTrackerIndex(ti)},
				FetchedAt:    time.Now(),
			})
			assert.NotNil(t, c.GetTrackerFromCacheNonBlocking(ti))
			assert.Empty(t, c.trackerRespErrors)
		})
	}
}
