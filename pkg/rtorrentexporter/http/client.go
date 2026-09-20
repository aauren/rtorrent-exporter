package http

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"time"
)

type ClientOpts struct {
	DialTimeout time.Duration
	Insecure    bool
	Password    string
	Username    string
}

type timeout struct {
	DialTimeout time.Duration
}

// An authRoundTripper is a http.RoundTripper which adds HTTP Basic authentication
// to each HTTP request.
type authRoundTripper struct {
	Username  string
	Password  string
	Transport *http.Transport
	Timeout   time.Duration
}

func NewRoundTripper(opts ClientOpts) http.RoundTripper {
	t := &timeout{DialTimeout: opts.DialTimeout}
	rt := &authRoundTripper{
		Timeout: opts.DialTimeout,
		Transport: &http.Transport{
			// We build the transport by hand rather than cloning http.DefaultTransport, so proxy support has to be asked for explicitly
			Proxy:       http.ProxyFromEnvironment,
			DialContext: t.dialTimeout,
			// The dial timeout alone lets an rtorrent that accepts and then hangs stall a scrape forever
			ResponseHeaderTimeout: opts.DialTimeout,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				//nolint:gosec // we don't care that this may be true, that's the point
				InsecureSkipVerify: opts.Insecure,
			},
		},
	}

	// Only carry credentials when we have both halves, so that RoundTrip knows it can skip the Authorization header entirely
	if opts.Username != "" && opts.Password != "" {
		rt.Username = opts.Username
		rt.Password = opts.Password
	}

	return rt
}

func (t *timeout) dialTimeout(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout: t.DialTimeout,
	}
	return dialer.DialContext(ctx, network, addr)
}

func (rt *authRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	req := r.Clone(r.Context())
	var cancel context.CancelFunc
	if rt.Timeout > 0 {
		var ctx context.Context
		ctx, cancel = context.WithTimeout(req.Context(), rt.Timeout)
		req = req.WithContext(ctx)
	}
	if rt.Username != "" && rt.Password != "" {
		req.SetBasicAuth(rt.Username, rt.Password)
	}

	resp, err := rt.Transport.RoundTrip(req)
	if cancel != nil {
		if err != nil {
			cancel()
		} else {
			// We keep the deadline alive until Close because RoundTrip returns before the body has been read
			resp.Body = &cancelBody{ReadCloser: resp.Body, cancel: cancel}
		}
	}
	return resp, err
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error {
	b.cancel()
	return b.ReadCloser.Close()
}
