package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseBodyTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		timeout time.Duration
		delay   time.Duration
		wantErr bool
	}{
		{name: "complete response", timeout: time.Second},
		{name: "stalled body", timeout: 50 * time.Millisecond, delay: 200 * time.Millisecond, wantErr: true},
		{name: "no timeout", delay: 100 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				select {
				case <-time.After(tt.delay):
				case <-r.Context().Done():
					return
				}
				_, _ = io.WriteString(w, "response body")
			}))
			t.Cleanup(srv.Close)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			require.NoError(t, err)
			rt := NewRoundTripper(ClientOpts{DialTimeout: tt.timeout})
			resp, err := rt.RoundTrip(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if tt.wantErr {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.NoError(t, ctx.Err(), "the transport deadline should fire before the caller's deadline")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "response body", string(body))
		})
	}
}

func TestNewRoundTripper(t *testing.T) {
	t.Parallel()

	// A server that accepts the connection and then sits on it should trip the timeout, not hang the scrape
	t.Run("times out waiting for response headers", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(500 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)

		rt := NewRoundTripper(ClientOpts{DialTimeout: 50 * time.Millisecond})
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
		require.NoError(t, err)

		start := time.Now()
		resp, err := rt.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}

		require.Error(t, err)
		assert.Less(t, time.Since(start), 400*time.Millisecond, "expected the request to give up well before the server responded")
	})

	t.Run("sends basic auth when both halves are set", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok || u != "user" || p != "pass" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)

		rt := NewRoundTripper(ClientOpts{DialTimeout: time.Second, Username: "user", Password: "pass"})
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
		require.NoError(t, err)

		resp, err := rt.RoundTrip(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
