package tracker

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/aauren/rtorrent-exporter/pkg/rtorrentexporter/config"
	"github.com/aauren/rtorrent/rtorrent"
	klog "k8s.io/klog/v2"
)

const (
	// The internal max poll interval is the maximum amount of time that we will wait between checking the cache for new items on blocking
	// fetch requests.
	internalMaxPollInterval = 50 * time.Millisecond
	// internalCacheCheckInterval is the interval at which we will check the cache check channel for new requests.
	internalCacheCheckInterval = 1 * time.Second
	// Maximum number of requests that will be cached. This prevents a non-blocking requestor from being blocked until an available fetcher
	// receives the request. All requests sent on this channel will be cache checked before sending to a fetcher, to ensure that the
	// fetcher's are not overwhelmed by duplicate requests.
	maxCacheCheckChanBuffer = 100000
	// The parallelRequestsBufferMultiplier is multiplied by the max parallel requests given by the user at runtime to determine the size of
	// the request channel buffer. This is to ensure that we can handle a burst of requests without blocking the fetchers.
	parallelRequestsBufferMultiplier = 10
	// unknownDomain is the domain that we report for a tracker whenever we're unable to work out its real domain.
	unknownDomain = "unknown"
)

// Cacher is a construct that caches the results of a Tracker Fetcher for a specified min / max time and according to a set number of
// maximum parallel requests.
type Cacher struct {
	// Needed for constructing the cacher
	f     Source
	cOpts CacheOpts

	// Needed for running fetch requesters
	reqChan        chan *FetchRequest
	cacheCheckChan chan *FetchRequest
	resChan        chan *TrackerResponse

	// Needed for caching
	trackerCache      map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance
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
	// TrackerNameSubstitutions is a list of tracker name substitutions that can be used to convert tracker names to a different name.
	TrackerNameSubstitutions []config.TrackerNameSubstitutions
}

type ModifiedTracker struct {
	Tracker           *rtorrent.Tracker
	SubstitutedDomain string
}

// TimedTrackerCacheInstance is a struct that holds a single tracker and the time that it was fetched. This is what is stored in the
// internal trackerCache. This is distinct from the TrackerResponse as it only holds a single tracker and the time that it was fetched and
// does not hold any errors that may have been encountered during the tracker request.
type TimedTrackerCacheInstance struct {
	// Tracker is a single tracker that was fetched.
	Tracker *ModifiedTracker
	// FetchedAt is the time that the tracker was fetched.
	FetchedAt time.Time
}

func (ttci *TimedTrackerCacheInstance) String() string {
	return fmt.Sprintf("TimedTrackerCacheInstance: fetched at: <%s>, Value: <%s>", ttci.FetchedAt, ttci.Tracker)
}

func (ttci *TimedTrackerCacheInstance) IsStale(minAge time.Duration, maxAge time.Duration) bool {
	if time.Since(ttci.FetchedAt) < minAge {
		return false
	}
	if time.Since(ttci.FetchedAt) > maxAge {
		return true
	}

	// Generate a random duration between minAge and maxAge
	//nolint:gosec // we don't care that we are using an insecure random number generator for this purpose
	randomDuration := minAge + time.Duration(rand.Int63n(int64(maxAge-minAge)))

	// Compare tr.FetchedAt against the current time minus the random duration
	return time.Since(ttci.FetchedAt) > randomDuration
}

// NewCacher creates a new Cacher with the specified Fetcher and CacheOpts. Everything a caller can touch is built here rather than in Run,
// so that a Cacher is fully usable the moment you have one. Otherwise a pre-warm or a scrape that lands before the scheduler gets around to
// running Run would send on a nil channel and block forever.
func NewCacher(f Source, opts CacheOpts) *Cacher {
	c := &Cacher{
		f:                 f,
		cOpts:             opts,
		reqChan:           make(chan *FetchRequest, opts.MaxParallelRequests*parallelRequestsBufferMultiplier),
		cacheCheckChan:    make(chan *FetchRequest, maxCacheCheckChanBuffer),
		resChan:           make(chan *TrackerResponse, opts.MaxParallelRequests*parallelRequestsBufferMultiplier),
		trackerCache:      make(map[rtorrent.TrackerIndex]*TimedTrackerCacheInstance),
		trackerRespErrors: make(map[rtorrent.TrackerIndex]error),
	}
	c.blockingReqCtx, c.blockingReqCancel = context.WithCancel(context.Background())

	return c
}

// getTrackerFromCacheOnly retrieves a tracker from the cache without sending a fetch request if the item doesn't exist, this is meant to be
// an internal method only.
func (c *Cacher) getTrackerFromCacheOnly(ti *rtorrent.TrackerIndex) (*TimedTrackerCacheInstance, bool, error) {
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

// getBlockingReqCtx returns the current broadcast cancellation context. It takes the lock because cacheTrackersError swaps the context out
// from under us whenever it has to bail out every in-flight blocking request at once.
func (c *Cacher) getBlockingReqCtx() context.Context {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	return c.blockingReqCtx
}

// GetTrackerFromCacheNonBlocking retrieves a tracker from the cache, if it doesn't exist, it will send a fetch request and return nil. In
// the case of stale cached data, it will return stale data and send a fetch request.
func (c *Cacher) GetTrackerFromCacheNonBlocking(ti *rtorrent.TrackerIndex) *ModifiedTracker {
	klog.V(2).Infof("got non-blocking tracker request for: %v", ti)
	ttci, ok, _ := c.getTrackerFromCacheOnly(ti)
	if !ok {
		klog.V(2).Infof("tracker was not found in cache, sending request to cache check channel: %v", ti)
		fr := &FetchRequest{TrackerIndex: ti}
		c.cacheCheckChan <- fr
		return nil
	}

	if ttci.IsStale(c.cOpts.MinAge, c.cOpts.MaxAge) {
		klog.V(1).Infof("tracker was found in cache but is stale, returning existing entry, but sending request to fetcher: %v", ti)
		fr := &FetchRequest{TrackerIndex: ti}
		c.reqChan <- fr
	}

	klog.V(2).Infof("tracker was found in cache, returning existing entry: %v", ti)
	return ttci.Tracker
}

// GetTrackerFromCacheBlocking retrieves a tracker from the cache, if it doesn't exist, it will send a fetch request and block until the
// tracker is available, and then return the tracker. In the case of stale cached data, it will return stale data and send a fetch request
func (c *Cacher) GetTrackerFromCacheBlocking(ctx context.Context, ti *rtorrent.TrackerIndex) (*ModifiedTracker, error) {
	klog.V(2).Infof("got blocking tracker request for: %v", ti)
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

	klog.V(1).Infof("tracker was not found in cache, blocking until tracker is available: %v", ti)
	// As of this moment take the pointer to the blocking request context, this will be used to cancel the blocking request in case of
	// emergency, if needed.
	blockingCancelCtx := c.getBlockingReqCtx()

	// If we're unable to get a non-blocking version of the tracker from the cache, then we need to block until we get a response added to
	// the cache.
	ticker := time.NewTicker(internalMaxPollInterval)
	for {
		select {
		case <-ticker.C:
			klog.V(2).Infof("checking cache for tracker: %v", ti)
			tr, ok, err := c.getTrackerFromCacheOnly(ti)
			if ok {
				klog.V(1).Infof("blocking tracker request was found in cache, returning existing entry: %v", ti)
				ticker.Stop()
				return tr.Tracker, nil
			}
			if err != nil {
				klog.V(1).Infof("blocking tracker request was found in cache, but with an error, returning error: %v", err)
				ticker.Stop()
				return nil, err
			}
		case <-ctx.Done():
			klog.Infof("blocking request context has been cancelled, or timedout: %v", ti)
			ticker.Stop()
			return nil, ctx.Err()
		case <-blockingCancelCtx.Done():
			klog.Infof("all blocking requests have been cancelled for an unknown reason, cancelling request: %v", ti)
			ticker.Stop()
			return nil, fmt.Errorf("blocking request was cancelled for an unknown error, try request again")
		}
	}
}

func (c *Cacher) parseModifiedTracker(t *rtorrent.Tracker) *ModifiedTracker {
	// Start off by creating the modified tracker with an embedded tracker instance and an unknown domain
	mt := &ModifiedTracker{
		Tracker:           t,
		SubstitutedDomain: unknownDomain,
	}

	// Attempt to get the Domain from the tracker, any errors cause mt to be returned as is
	url, err := t.URL()
	if err != nil {
		klog.Errorf("error getting URL for tracker (Tracker Index Hash: %s): %v", t.TrackerIndex().String(), err)
		return mt
	}
	mt.SubstitutedDomain, err = GetDomainForTrackerURL(url)
	if err != nil {
		klog.Errorf("error getting domain for tracker URL: %v", err)
		return mt
	}

	// If there are no substitutions, then we can return the tracker as is
	if len(c.cOpts.TrackerNameSubstitutions) < 1 {
		return mt
	}

	for _, sub := range c.cOpts.TrackerNameSubstitutions {
		for _, matcher := range sub.CompiledMathers {
			if matcher.MatchString(mt.SubstitutedDomain) {
				mt.SubstitutedDomain = sub.ConvertTo
				return mt
			}
		}
	}
	return mt
}

// cacheTracker caches a tracker response and clears any error responses now that we have a successful response, this is meant to be an
// internal method only.
func (c *Cacher) cacheTrackers(tr *TrackerResponse) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// In the case that there is more than one tracker returned, then it is likely that the TrackerIndex which perviously only contained
	// a hash now contains a hash an index, so we need to make a non-indexed one and add it so that we cache the error for the non-indexed
	// version that was originally looked up.
	ts := trackerSliceEnsuringTrackerWithHashOnly(tr)

	for _, t := range ts {
		mt := c.parseModifiedTracker(t)

		// Create a new TimedTrackerCacheInstance
		ttci := &TimedTrackerCacheInstance{
			Tracker:   mt,
			FetchedAt: tr.FetchedAt,
		}

		// Add the current result into the tracker cache, overriding any existing result that may be there
		c.trackerCache[*t.TrackerIndex()] = ttci

		// Purge any errors that may have been obtained for this tracker in the past
		delete(c.trackerRespErrors, *t.TrackerIndex())
	}
}

// In the case that there is more than one tracker returned, then it is likely that the TrackerIndex which perviously only contained a hash
// now contains a hash an index, so we need to make a non-indexed one and add it so that we cache the error for the non-indexed version that
// was originally looked up.
func trackerSliceEnsuringTrackerWithHashOnly(tr *TrackerResponse) []*rtorrent.Tracker {
	// If there is only one tracker in the response, then we don't need to do anything.
	if len(tr.Trackers) == 1 {
		return tr.Trackers
	}

	// First ensure that Trackers slice inside TrackerResponse are sorted so that they are in a consistent order
	sort.Sort(tr)

	// Take a copy of the trackers in the response as we don't want to append to the original list of Trackers inside the TrackerResponse
	trackers := make([]*rtorrent.Tracker, len(tr.Trackers))
	copy(trackers, tr.Trackers)

	// Now get the first tracker and create a new tracker with the same hash but no index
	firstTracker := trackers[0]
	firstTrackerNoIndex := firstTracker.CloneWithTrackerIndex(rtorrent.NewTrackerNoIndex(firstTracker.TrackerIndex().InfoHash))

	// Check the existing list of Trackers in the response to ensure that a tracker with the same hash but no index doesn't already
	// exist, if it does, then we don't need to add it again.
	for _, t := range trackers {
		if t.TrackerIndex().String() == firstTrackerNoIndex.TrackerIndex().String() {
			return trackers
		}
	}

	// Add it to the list of trackers in the Tracker response
	trackers = append(trackers, firstTrackerNoIndex)
	return trackers
}

// cacheTrackersError caches an error for a tracker, this is meant to be an internal method only. We have to be careful as the Tracker may
// be nil for certain error cases.
func (c *Cacher) cacheTrackersError(tr *TrackerResponse) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// These are the error case in which the tracker may be nil, these are somewhat special, in that since the tracker is nil we can't
	// register the error in the error map, so in order to ensure that the request didn't come from a blocking requester where we would
	// have the chance of blocking forever, we have to do a nuclear option and signal all blocking requests that they should return with
	// an error.
	err := tr.Error
	nilTracker := errors.Is(err, rtorrent.ErrNilTrackerIndex)
	if nilTracker {
		klog.Errorf("there was an error fetching the requested tracker (ti was likely nil: %v): %v", tr, err)
		// Cancel all blocking requests that are currently in flight
		c.blockingReqCancel()
		// Setup new blocking request context / cancel function so that we can bail out again in the future if needed
		c.blockingReqCtx, c.blockingReqCancel = context.WithCancel(context.Background())
		return
	}

	ts := trackerSliceEnsuringTrackerWithHashOnly(tr)

	// Iterate over all trackers in the response and cache the error for each one. Additionally, we need to purge any data that may have
	// been stored in the cache for this tracker in the past.
	for _, t := range ts {
		// Add the current error into the tracker error cache, overriding any existing error that may be there
		c.trackerRespErrors[*t.TrackerIndex()] = err

		// Purge any tracker that may have been obtained for this tracker in the past
		delete(c.trackerCache, *t.TrackerIndex())
	}
}

// checkCacheForStaleItems checks the cache for stale items and sends a request to the fetcher to refresh the cache if the item is stale.
func (c *Cacher) checkCacheForStaleItems(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	klog.V(2).Infof("checking cache for stale items")
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	for ti, tr := range c.trackerCache {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if tr.IsStale(c.cOpts.MinAge, c.cOpts.MaxAge) {
			klog.V(1).Infof("tracker was found in cache but is stale sending request to fetcher: %v", ti)
			fr := &FetchRequest{TrackerIndex: &ti}
			select {
			case c.reqChan <- fr:
				klog.V(1).Info("successfully sent request to fetcher")
			default:
				klog.Warningf("fetcher request channel is full, unable to send request: %v", ti)
			}
		}
	}
}

// checkCacheCheckChan checks the cache check channel for new requests and sends them to the fetcher if they are not satisfied by the cache.
func (c *Cacher) checkCacheCheckChan() {
	for {
		select {
		case fr := <-c.cacheCheckChan:
			tr, ok, _ := c.getTrackerFromCacheOnly(fr.TrackerIndex)
			if !ok || tr == nil || tr.IsStale(c.cOpts.MinAge, c.cOpts.MaxAge) {
				select {
				case c.reqChan <- fr:
					klog.V(1).Infof("sent request to fetcher for tracker: %v", fr.TrackerIndex)
				default:
					klog.V(2).Infof("fetcher request channel is full, re-queuing the request: %v", fr.TrackerIndex)
					c.cacheCheckChan <- fr
					return
				}
				continue
			}
			klog.V(3).Infof("tracker was found in cache, not sending request to fetcher: %v", fr.TrackerIndex)
		default:
			return
		}
	}
}

// Run starts the cacher and runs the main loop for the cacher.
func (c *Cacher) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	childWG := &sync.WaitGroup{}

	// Setup fetchers up to the maximum number of allowed parallel requests
	for i := 0; i < c.cOpts.MaxParallelRequests; i++ {
		f := NewFetcher(c.f)
		childWG.Add(1)
		go f.Run(ctx, childWG, c.reqChan, c.resChan)
	}

	// Setup a timer to check cached items for staleness proactively
	cacheCheckTicker := time.NewTicker(c.cOpts.MinAge)
	defer cacheCheckTicker.Stop()
	cacheCheckChanTicker := time.NewTicker(internalCacheCheckInterval)
	defer cacheCheckChanTicker.Stop()

	// Setup cacher loop
	for {
		select {
		// If we're all done here, then wait for our children and return
		case <-ctx.Done():
			klog.Info("cacher has been asked to stop, stopping...")
			// We deliberately don't close reqChan or resChan here. Cancelling the context is what stops the fetchers, and in-flight
			// stale checks or a concurrent scrape coming through GetTrackerFromCacheNonBlocking can still send on reqChan, which
			// would panic on a closed channel.
			childWG.Wait()
			klog.Info("cacher has stopped")
			return
		// If we have a response from the fetcher, then check for errors and cache it if appropriate
		case resp := <-c.resChan:
			klog.V(3).Infof("received response from fetcher for tracker: %v", resp)
			if resp.Error != nil {
				c.cacheTrackersError(resp)
				continue
			}
			c.cacheTrackers(resp)
		// If our ticker ticks, check cache for stale items proactively
		case <-cacheCheckTicker.C:
			klog.V(1).Infof("checking cache for stale items")
			childWG.Add(1)
			go c.checkCacheForStaleItems(ctx, childWG)
		// Moderate our cacheCheckChan to see if we have any requests for new fetches, then check them against the cache, if they are not
		// yet satisfied by cache, then send them on to the fetchers
		case <-cacheCheckChanTicker.C:
			klog.V(3).Infof("checking cache check channel for new requests")
			c.checkCacheCheckChan()
		}
	}
}
