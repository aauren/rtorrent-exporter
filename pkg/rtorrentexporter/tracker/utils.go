package tracker

import (
	"errors"
	"sort"

	"github.com/aauren/rtorrent/rtorrent"
)

var (
	errNilTracker = errors.New("tracker is nil")
	errNoURLs     = errors.New("tracker has no URLs")
)

func GetSingleTrackerURL(t *rtorrent.Tracker) (string, error) {
	if t == nil {
		return "", errNilTracker
	}

	urls, err := t.URLs()
	if err != nil {
		return "", err
	}

	if len(urls) < 1 {
		return "", errNoURLs
	}

	// Ensure that the strings are sorted so that we get consistent results, and not just whatever order rtorrent returned them in
	sort.Strings(urls)

	for _, url := range urls {
		if url != "" {
			return url, nil
		}
	}

	return "", errNoURLs
}
