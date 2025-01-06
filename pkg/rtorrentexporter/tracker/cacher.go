package tracker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aauren/rtorrent/rtorrent"
	klog "k8s.io/klog/v2"
)

const (
	internalMaxPollInterval = 50 * time.Millisecond
)

// Cacher is a construct that caches the results of a Tracker Fetcher for a specified min / max time and according to a set number of
// maximum parallel requests.
type Cacher struct {
	// Needed for constructing the cacher
	f     Source
	cOpts CacheOpts

	// Needed for running fetch requesters
	reqChan chan *FetchRequest
	resChan chan *TrackerResponse

	// Needed for caching
	trackerCache      map[rtorrent.TrackerIndex]*TrackerResponse
	trackerRespErrors map[rtorrent.TrackerIndex]error
	cacheMu           sync.RWMutex
	blockingReqCtx    context.Context
	blockingReqCancel context.CancelFunc
}

// CacheOpts is a struct that contains the options for a Cacher.
type CacheOpts struct {
	// MaxAge is the maximum age of a cached item before it is considered stale.
	MaxAge time.Duration
	// MinAge is the minimum age of a cached item before it is considered fresh.
	MinAge time.Duration
	// MaxParallelRequests is the maximum number of parallel requests that can be made to the fetcher.
	MaxParallelRequests int
}

// NewCacher creates a new Cacher with the specified Fetcher and CacheOpts.
func NewCacher(f Source, opts CacheOpts) *Cacher {
	return &Cacher{f: f, cOpts: opts}
}

// getTrackerFromCacheOnly retrieves a tracker from the cache without sending a fetch request if the item doesn't exist, this is meant to be
// an internal method only.
func (c *Cacher) getTrackerFromCacheOnly(ti *rtorrent.TrackerIndex) (*TrackerResponse, bool, error) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	tr, ok := c.trackerCache[*ti]
	if ok {
		return tr, true, nil
	}

	err, ok := c.trackerRespErrors[*ti]
	if ok && err != nil {
		return nil, false, err
	}

	return nil, false, nil
}

// GetTrackerFromCacheNonBlocking retrieves a tracker from the cache, if it doesn't exist, it will send a fetch request and return nil. In
// the case of stale cached data, it will return stale data and send a fetch request.
func (c *Cacher) GetTrackerFromCacheNonBlocking(ti *rtorrent.TrackerIndex) *rtorrent.Tracker {
	tr, ok, _ := c.getTrackerFromCacheOnly(ti)
	if !ok {
		fr := &FetchRequest{TrackerIndex: ti}
		c.reqChan <- fr
		return nil
	}

	if tr.IsStale(c.cOpts.MaxAge, c.cOpts.MinAge) {
		fr := &FetchRequest{TrackerIndex: ti}
		c.reqChan <- fr
	}

	return tr.Tracker
}

// GetTrackerFromCacheBlocking retrieves a tracker from the cache, if it doesn't exist, it will send a fetch request and block until the
// tracker is available, and then return the tracker. In the case of stale cached data, it will return stale data and send a fetch request
func (c *Cacher) GetTrackerFromCacheBlocking(ctx context.Context, ti *rtorrent.TrackerIndex) (*rtorrent.Tracker, error) {
	// This should help protect us from nil tracker problems later on
	if ti == nil {
		return nil, fmt.Errorf("tracker index cannot be nil")
	}

	// See if we're able to get a non-blocking version of this tracker from the cache first, if not this will also send our fetch request
	// to get a version added to the cache.
	tr := c.GetTrackerFromCacheNonBlocking(ti)
	if tr != nil {
		return tr, nil
	}

	// As of this moment take the pointer to the blocking request context, this will be used to cancel the blocking request if needed
	blockingCancelCtx := c.blockingReqCtx

	// If we're unable to get a non-blocking version of the tracker from the cache, then we need to block until we get a response added to
	// the cache.
	ticker := time.NewTicker(internalMaxPollInterval)
	for {
		select {
		case <-ticker.C:
			tr, ok, err := c.getTrackerFromCacheOnly(ti)
			if ok {
				ticker.Stop()
				return tr.Tracker, nil
			}
			if err != nil {
				ticker.Stop()
				return nil, err
			}
		case <-ctx.Done():
			ticker.Stop()
			return nil, ctx.Err()
		case <-blockingCancelCtx.Done():
			ticker.Stop()
			return nil, fmt.Errorf("blocking request was cancelled for an unknown error, try request again")
		}
	}
}

func (c *Cacher) cacheTracker(tr *TrackerResponse) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// Add the current result into the tracker cache, overriding any existing result that may be there
	c.trackerCache[*tr.Tracker.TrackerIndex()] = tr

	// Purge any errors that may have been obtained for this tracker in the past
	delete(c.trackerRespErrors, *tr.Tracker.TrackerIndex())
}

// cacheTrackerError caches an error for a tracker, this is meant to be an internal method only. We have to be careful as the Tracker may be
// nil for certain error cases.
func (c *Cacher) cacheTrackerError(t *rtorrent.Tracker, err error) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// These are the error case in which the tracker may be nil, these are somewhat special, in that since the tracker is nil we can't
	// register the error in the error map, so in order to ensure that the request didn't come from a blocking requester where we would
	// have the chance of blocking forever, we have to do a nuclear option and signal all blocking requests that they should return with
	// an error.
	nilTracker := errors.Is(err, rtorrent.ErrNilTrackerIndex) || t == nil
	if nilTracker {
		klog.Errorf("there was an error fetching the requested tracker (ti was likely nil: %v): %v", t, err)
		// Cancel all blocking requests that are currently in flight
		c.blockingReqCancel()
		// Setup new blocking request context / cancel function so that we can bail out again in the future if needed
		c.blockingReqCtx, c.blockingReqCancel = context.WithCancel(context.Background())
		return
	}

	// Add the current error into the tracker error cache, overriding any existing error that may be there
	c.trackerRespErrors[*t.TrackerIndex()] = err

	// Purge any tracker that may have been obtained for this tracker in the past
	delete(c.trackerCache, *t.TrackerIndex())
}

func (c *Cacher) checkCacheForStaleItems(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	c.cacheMu.RLock()
	defer c.cacheMu.Unlock()

	for ti, tr := range c.trackerCache {
		if ctx.Done() != nil {
			return
		}
		if tr.IsStale(c.cOpts.MaxAge, c.cOpts.MinAge) {
			c.GetTrackerFromCacheNonBlocking(&ti)
		}
	}
}

func (c *Cacher) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	childWG := &sync.WaitGroup{}
	c.reqChan = make(chan *FetchRequest)
	c.resChan = make(chan *TrackerResponse)
	c.blockingReqCtx, c.blockingReqCancel = context.WithCancel(context.Background())

	// Setup fetchers up to the maximum number of allowed parallel requests
	for i := 0; i < c.cOpts.MaxParallelRequests; i++ {
		f := NewFetcher(c.f)
		childWG.Add(1)
		go f.Run(ctx, childWG, c.reqChan, c.resChan)
	}

	// Setup a timer to check cached items for staleness proactively
	cacheCheckTicker := time.NewTicker(c.cOpts.MinAge)

	// Setup cacher loop
	for {
		select {
		// If we're all done here, then close out our channels and return
		case <-ctx.Done():
			// Close all fetchers
			close(c.reqChan)
			// Wait for all fetchers to finish
			childWG.Wait()
			close(c.resChan)
			return
		// If we have a response from the fetcher, then check for errors and cache it if appropriate
		case resp := <-c.resChan:
			if resp.Error != nil {
				c.cacheTrackerError(resp.Tracker, resp.Error)
			}
			c.cacheTracker(resp)
		// If our ticker ticks, check cache for stale items proactively
		case <-cacheCheckTicker.C:
			childWG.Add(1)
			go c.checkCacheForStaleItems(ctx, childWG)
		}
	}
}
