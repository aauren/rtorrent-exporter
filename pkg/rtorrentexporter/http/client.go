package http

import (
	"context"
	"crypto/tls"
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
}

func NewRoundTripper(opts ClientOpts) http.RoundTripper {
	t := &timeout{DialTimeout: opts.DialTimeout}
	rt := &authRoundTripper{
		Transport: &http.Transport{
			// We build the transport by hand rather than cloning http.DefaultTransport, so proxy support has to be asked for explicitly
			Proxy:       http.ProxyFromEnvironment,
			DialContext: t.dialTimeout,
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
	// Without a full set of credentials there's nothing to add, so we hand the request straight through rather than sending a partial or
	// empty Basic header. The constructor only ever sets both or neither, but || is the defensive choice against direct struct construction
	if rt.Username == "" || rt.Password == "" {
		return rt.Transport.RoundTrip(r)
	}

	// RoundTrip isn't allowed to modify the request it was given, so we set the header on a clone
	req := r.Clone(r.Context())
	req.SetBasicAuth(rt.Username, rt.Password)

	return rt.Transport.RoundTrip(req)
}
