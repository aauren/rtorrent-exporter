package tracker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aauren/rtorrent/rtorrent"
	klog "k8s.io/klog/v2"
)

// Source is an interface that provides tracker information.
type Source interface {
	TrackerWithDetails(ctx context.Context, ti *rtorrent.TrackerIndex, fields []rtorrent.TrackerField) ([]*rtorrent.Tracker, error)
}

// FetchRequest is a construct that contains the request to fetch tracker information.
type FetchRequest struct {
	// TrackerIndex contains the hash and optionally an index as well to identify the tracker.
	*rtorrent.TrackerIndex
	// Fields is a slice of fields that should be fetched. If this is empty, then all fields will be fetched.
	Fields []rtorrent.TrackerField
}

// TrackerResponse is a construct that contains the response from a tracker FetchRequest
type TrackerResponse struct {
	// Trackers is a slice of trackers that were fetched. If an index was passed in the TrackerIndex object, then it should only ever
	// contain a single Tracker. However, if only a hash was passed, then it may contain multiple trackers.
	Trackers []*rtorrent.Tracker
	// Error is any errors that were encountered while fetching the trackers.
	Error error
	// FetchedAt is the time that the trackers were fetched.
	FetchedAt time.Time
}

func (tr *TrackerResponse) String() string {
	if tr.Error != nil {
		return fmt.Sprintf("TrackerResponse: fetched at: <%s>, Error: <%s>", tr.FetchedAt, tr.Error.Error())
	}
	var sb strings.Builder
	for _, t := range tr.Trackers {
		fmt.Fprintf(&sb, "\nTrackerResponse: fetched at: <%s>, Value: <%s>", tr.FetchedAt, t)
	}
	return sb.String()
}

// Fetcher is a construct that retrieves tracker information from a Source.
type Fetcher struct {
	ts Source
}

// NewFetcher creates a new Fetcher with the specified Source.
func NewFetcher(ts Source) *Fetcher {
	return &Fetcher{ts: ts}
}

// GetTrackersByHashIndexAllFields retrieves all available fields for a tracker (identified by hash and index). This is implemented as a
// helper function to avoid having to create a TrackerIndex object.
func (f *Fetcher) GetTrackersByHashIndexAllFields(ctx context.Context, hash string, index int) ([]*rtorrent.Tracker, error) {
	ti := rtorrent.NewTrackerWithIndex(hash, index)
	return f.ts.TrackerWithDetails(ctx, ti, rtorrent.AllTrackerFields())
}

// GetTrackersByHashAllFields retrieves all available fields for a tracker (identified by hash). This is implemented as a helper function to
// avoid having to create a TrackerIndex object.
func (f *Fetcher) GetTrackersByHashAllFields(ctx context.Context, hash string) ([]*rtorrent.Tracker, error) {
	ti := rtorrent.NewTrackerNoIndex(hash)
	return f.ts.TrackerWithDetails(ctx, ti, rtorrent.AllTrackerFields())
}

// GetTrackersAllFields retrieves all available fields for a tracker (identified by hash and index).
func (f *Fetcher) GetTrackersAllFields(ctx context.Context, ti *rtorrent.TrackerIndex) ([]*rtorrent.Tracker, error) {
	return f.ts.TrackerWithDetails(ctx, ti, rtorrent.AllTrackerFields())
}

// GetTrackersByHashIndexSelectedFields retrieves the specified fields for a tracker (identified by hash and index). This is implemented as
// a helper function to avoid having to create a TrackerIndex object.
func (f *Fetcher) GetTrackersByHashIndexSelectedFields(ctx context.Context, hash string, index int,
	fields []rtorrent.TrackerField) ([]*rtorrent.Tracker, error) {
	ti := rtorrent.NewTrackerWithIndex(hash, index)
	return f.ts.TrackerWithDetails(ctx, ti, fields)
}

// GetTrackersByHashSelectedFields retrieves the specified fields for a tracker (identified by hash). This is implemented as a helper
// function to avoid having to create a TrackerIndex object.
func (f *Fetcher) GetTrackersByHashSelectedFields(ctx context.Context, hash string,
	fields []rtorrent.TrackerField) ([]*rtorrent.Tracker, error) {
	ti := rtorrent.NewTrackerNoIndex(hash)
	return f.ts.TrackerWithDetails(ctx, ti, fields)
}

// GetTrackersSelectedFields retrieves the specified fields for a tracker (identified by hash and index).
func (f *Fetcher) GetTrackersSelectedFields(ctx context.Context, ti *rtorrent.TrackerIndex,
	fields []rtorrent.TrackerField) ([]*rtorrent.Tracker, error) {
	return f.ts.TrackerWithDetails(ctx, ti, fields)
}

func (f *Fetcher) Run(ctx context.Context, inCH <-chan *FetchRequest, outCH chan<- *TrackerResponse) {
	klog.Infof("starting tracker fetcher thread")

	for {
		select {
		case <-ctx.Done():
			klog.Infof("stopping tracker fetcher thread")
			return
		case req := <-inCH:
			// A request that names no fields is asking for all of them
			fields := req.Fields
			if len(fields) == 0 {
				fields = rtorrent.AllTrackerFields()
			}

			resp, err := f.GetTrackersSelectedFields(ctx, req.TrackerIndex, fields)
			// Guard the send with the context, because the cacher stops draining outCH once it's cancelled, and a bare send on a
			// full buffer would then hang this goroutine and stall shutdown forever
			select {
			case outCH <- &TrackerResponse{Trackers: resp, Error: err, FetchedAt: time.Now()}:
			case <-ctx.Done():
				klog.Infof("stopping tracker fetcher thread")
				return
			}
		}
	}
}
