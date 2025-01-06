package tracker

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/aauren/rtorrent/rtorrent"
)

// Source is an interface that provides tracker information.
type Source interface {
	TrackerWithDetails(ctx context.Context, ti *rtorrent.TrackerIndex, fields []rtorrent.TrackerField) (*rtorrent.Tracker, error)
}

type FetchRequest struct {
	*rtorrent.TrackerIndex
	Fields []rtorrent.TrackerField
}

type TrackerResponse struct {
	*rtorrent.Tracker
	Error     error
	FetchedAt time.Time
}

func (tr *TrackerResponse) IsStale(minAge time.Duration, maxAge time.Duration) bool {
	if time.Since(tr.FetchedAt) < minAge {
		return false
	}
	if time.Since(tr.FetchedAt) > maxAge {
		return true
	}

	// Generate a random duration between minAge and maxAge
	//nolint:gosec // we don't care that we are using an insecure random number generator for this purpose
	randomDuration := minAge + time.Duration(rand.Int63n(int64(maxAge-minAge)))

	// Compare tr.FetchedAt against the current time minus the random duration
	return time.Since(tr.FetchedAt) > randomDuration
}

// Fetcher is a construct that retrieves tracker information from a Source.
type Fetcher struct {
	ts Source
}

// NewFethcer creates a new Fetcher with the specified Source.
func NewFetcher(ts Source) *Fetcher {
	return &Fetcher{ts: ts}
}

// GetTrackerByHashIndexAllFields retrieves all available fields for a tracker (identified by hash and index). This is implemented as a
// helper function to avoid having to create a TrackerIndex object.
func (f *Fetcher) GetTrackerByHashIndexAllFields(ctx context.Context, hash string, index int) (*rtorrent.Tracker, error) {
	ti := rtorrent.NewTrackerWithIndex(hash, index)
	return f.ts.TrackerWithDetails(ctx, ti, rtorrent.AllTrackerFields)
}

// GetTrackerByHashAllFields retrieves all available fields for a tracker (identified by hash). This is implemented as a helper function to
// avoid having to create a TrackerIndex object.
func (f *Fetcher) GetTrackerByHashAllFields(ctx context.Context, hash string) (*rtorrent.Tracker, error) {
	ti := rtorrent.NewTrackerNoIndex(hash)
	return f.ts.TrackerWithDetails(ctx, ti, rtorrent.AllTrackerFields)
}

// GetTrackerAllFields retrieves all available fields for a tracker (identified by hash and index).
func (f *Fetcher) GetTrackerAllFields(ctx context.Context, ti *rtorrent.TrackerIndex) (*rtorrent.Tracker, error) {
	return f.ts.TrackerWithDetails(ctx, ti, rtorrent.AllTrackerFields)
}

// GetTrackerByHashIndexSelectedFields retrieves the specified fields for a tracker (identified by hash and index). This is implemented as a
// helper function to avoid having to create a TrackerIndex object.
func (f *Fetcher) GetTrackerByHashIndexSelectedFields(ctx context.Context, hash string, index int,
	fields []rtorrent.TrackerField) (*rtorrent.Tracker, error) {
	ti := rtorrent.NewTrackerWithIndex(hash, index)
	return f.ts.TrackerWithDetails(ctx, ti, fields)
}

// GetTrackerByHashSelectedFields retrieves the specified fields for a tracker (identified by hash). This is implemented as a helper
// function to avoid having to create a TrackerIndex object.
func (f *Fetcher) GetTrackerByHashSelectedFields(ctx context.Context, hash string,
	fields []rtorrent.TrackerField) (*rtorrent.Tracker, error) {
	ti := rtorrent.NewTrackerNoIndex(hash)
	return f.ts.TrackerWithDetails(ctx, ti, fields)
}

// GetTrackerSelectedFields retrieves the specified fields for a tracker (identified by hash and index).
func (f *Fetcher) GetTrackerSelectedFields(ctx context.Context, ti *rtorrent.TrackerIndex,
	fields []rtorrent.TrackerField) (*rtorrent.Tracker, error) {
	return f.ts.TrackerWithDetails(ctx, ti, fields)
}

func (f *Fetcher) Run(ctx context.Context, wg *sync.WaitGroup, inCH <-chan *FetchRequest, outCH chan<- *TrackerResponse) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case req := <-inCH:
			// If fields are specified, then use them when making the request
			if len(req.Fields) > 0 {
				resp, err := f.GetTrackerSelectedFields(ctx, req.TrackerIndex, req.Fields)
				TrackerResponse := &TrackerResponse{Tracker: resp, Error: err, FetchedAt: time.Now()}
				outCH <- TrackerResponse
				continue
			}
			// If no fields are specified, then retrieve all fields
			resp, err := f.GetTrackerAllFields(ctx, req.TrackerIndex)
			TrackerResponse := &TrackerResponse{Tracker: resp, Error: err, FetchedAt: time.Now()}
			outCH <- TrackerResponse
		}
	}
}
