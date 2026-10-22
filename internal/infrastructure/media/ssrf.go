package iso

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
)

// CheckURL permits private mirrors but rejects loopback, unspecified and
// link-local destinations, including IPv4-mapped IPv6 addresses. All resolved
// addresses must pass before any network connection is made.
func CheckURL(u *url.URL, ips []netip.Addr) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || len(ips) == 0 {
		return sourceRejected("", "", isoSourceReasonURL)
	}
	for _, ip := range ips {
		ip = ip.Unmap()
		if !ip.IsValid() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
			return sourceRejected("", "", isoSourceReasonURL)
		}
	}
	return nil
}

// validatedTransport resolves once per HTTP request (including every redirect),
// validates every address, and connects only to those addresses. The transport
// still uses the URL host for the Host header, TLS SNI and certificate checks.
// No proxy or shared connection pool can bypass the per-request validation.
type validatedTransport struct{ sources *Sources }

func (v validatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	ips, err := v.sources.lookup(req.Context(), host)
	if err != nil {
		return nil, err
	}
	if err = CheckURL(req.URL, ips); err != nil {
		return nil, err
	}
	port := req.URL.Port()
	if port == "" {
		if req.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		var errs []error
		for _, ip := range ips {
			conn, e := v.sources.dialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
			errs = append(errs, e)
			if ctx.Err() != nil {
				break
			}
		}
		return nil, errors.Join(errs...)
	}}
	resp, err := tr.RoundTrip(req)
	tr.CloseIdleConnections()
	return resp, err
}

func (s *Sources) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	return s.resolver(ctx, host)
}

func (s *Sources) httpClient() *http.Client {
	return &http.Client{Transport: validatedTransport{sources: s}}
}
