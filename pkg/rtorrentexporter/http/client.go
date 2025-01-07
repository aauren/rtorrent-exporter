package http

import (
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

func NewRoundTripper(opts ClientOpts) *http.RoundTripper {
	var rt http.RoundTripper
	t := &timeout{DialTimeout: opts.DialTimeout}
	if opts.Username != "" && opts.Password != "" {
		rt = &authRoundTripper{
			Username: opts.Username,
			Password: opts.Password,
			Transport: &http.Transport{
				Dial: t.dialTimeout,
				TLSClientConfig: &tls.Config{
					//nolint:gosec // we don't care that this may be true, that's the point
					InsecureSkipVerify: opts.Insecure,
				},
			},
		}
	} else {
		rt = &authRoundTripper{
			Transport: &http.Transport{
				Dial: t.dialTimeout,
				TLSClientConfig: &tls.Config{
					//nolint:gosec // we don't care that this may be true, that's the point
					InsecureSkipVerify: opts.Insecure,
				},
			},
		}
	}
	return &rt
}

func (t *timeout) dialTimeout(network, addr string) (net.Conn, error) {
	return net.DialTimeout(network, addr, t.DialTimeout)
}

func (rt *authRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r.SetBasicAuth(rt.Username, rt.Password)
	return rt.Transport.RoundTrip(r)
}
