package tools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var restrictedCIDRs = []*net.IPNet{
	mustParseCIDR("0.0.0.0/8"),          // Current network (only valid as source)
	mustParseCIDR("10.0.0.0/8"),         // Private network RFC 1918
	mustParseCIDR("100.64.0.0/10"),      // Shared Address Space / CGNAT RFC 6598
	mustParseCIDR("127.0.0.0/8"),        // Loopback
	mustParseCIDR("169.254.0.0/16"),     // Link-Local (AWS/GCP/Azure metadata)
	mustParseCIDR("172.16.0.0/12"),      // Private network RFC 1918
	mustParseCIDR("192.0.0.0/24"),       // IETF Protocol Assignments RFC 6890
	mustParseCIDR("192.0.2.0/24"),       // Documentation (TEST-NET-1) RFC 5737
	mustParseCIDR("192.88.99.0/24"),     // 6to4 Relay Anycast RFC 7526
	mustParseCIDR("192.168.0.0/16"),     // Private network RFC 1918
	mustParseCIDR("198.18.0.0/15"),      // Benchmarking RFC 2544
	mustParseCIDR("198.51.100.0/24"),    // Documentation (TEST-NET-2) RFC 5737
	mustParseCIDR("203.0.113.0/24"),     // Documentation (TEST-NET-3) RFC 5737
	mustParseCIDR("224.0.0.0/4"),        // Multicast RFC 5771
	mustParseCIDR("240.0.0.0/4"),        // Reserved / Future use RFC 1112
	mustParseCIDR("255.255.255.255/32"), // Broadcast

	// IPv6
	mustParseCIDR("::/128"),        // Unspecified
	mustParseCIDR("::1/128"),       // Loopback
	mustParseCIDR("100::/64"),      // Discard-Only RFC 6666
	mustParseCIDR("2001:db8::/32"), // Documentation RFC 3849
	mustParseCIDR("fc00::/7"),      // Unique Local RFC 4193
	mustParseCIDR("fe80::/10"),     // Link-Local Unicast RFC 4291
	mustParseCIDR("ff00::/8"),      // Multicast RFC 4291
}

var restrictedHostnames = map[string]bool{
	"localhost":                true,
	"metadata.google.internal": true,
	"metadata.internal":        true,
	"instance-data":            true,
}

func mustParseCIDR(s string) *net.IPNet {
	_, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		panic(fmt.Sprintf("invalid CIDR %q: %v", s, err))
	}
	return ipNet
}

func isRestrictedHostname(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if restrictedHostnames[host] {
		return true
	}
	if strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return true
	}
	return false
}

// isRestrictedIP reports whether the given IP address is private, loopback,
// link-local, multicast, or cloud metadata.
func isRestrictedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}

	// Unwrap IPv4-mapped IPv6 address (e.g. ::ffff:127.0.0.1)
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}

	for _, cidr := range restrictedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}

	// Alibaba cloud metadata IP
	if ip.String() == "100.100.100.200" {
		return true
	}

	return false
}

// validateURLForSSRF checks that the URL uses an http/https scheme, has no credentials,
// and does not target restricted hostnames or private/metadata IP addresses.
func validateURLForSSRF(rawURL string) (*url.URL, error) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("read_url_content: invalid scheme, only http and https are supported")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("read_url_content: invalid url: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("read_url_content: invalid scheme %q, only http and https are supported", u.Scheme)
	}

	if u.User != nil {
		return nil, fmt.Errorf("read_url_content: credentials in url are not permitted for security")
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return nil, fmt.Errorf("read_url_content: empty host in url")
	}

	// Remove IPv6 enclosing brackets if present
	cleanHost := strings.TrimPrefix(strings.TrimSuffix(hostname, "]"), "[")

	// Check restricted hostnames
	if isRestrictedHostname(cleanHost) {
		return nil, fmt.Errorf("read_url_content: access to host %q is blocked by security policy (restricted/metadata hostname)", cleanHost)
	}

	// Check if hostname is an IP literal
	if ip := net.ParseIP(cleanHost); ip != nil {
		if isRestrictedIP(ip) {
			return nil, fmt.Errorf("read_url_content: access to IP address %s is blocked by security policy (private, loopback, or cloud metadata address)", ip.String())
		}
	}

	return u, nil
}

// newSafeHTTPClient constructs an HTTP client protected against SSRF, DNS rebinding,
// and malicious redirect destinations.
func newSafeHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			cleanHost := strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
			if isRestrictedHostname(cleanHost) {
				return nil, fmt.Errorf("connection to host %q blocked by SSRF policy", cleanHost)
			}

			var ips []net.IP
			if ip := net.ParseIP(cleanHost); ip != nil {
				ips = []net.IP{ip}
			} else {
				resolved, err := net.DefaultResolver.LookupIP(ctx, "ip", cleanHost)
				if err != nil {
					return nil, fmt.Errorf("dns lookup failed for %s: %w", cleanHost, err)
				}
				ips = resolved
			}

			// Find first non-restricted IP
			var targetIP net.IP
			for _, ip := range ips {
				if !isRestrictedIP(ip) {
					targetIP = ip
					break
				}
			}

			if targetIP == nil {
				return nil, fmt.Errorf("connection to %s blocked by SSRF policy: all resolved IPs are private or restricted", cleanHost)
			}

			dialer := &net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(targetIP.String(), port))
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if _, err := validateURLForSSRF(req.URL.String()); err != nil {
				return fmt.Errorf("redirect blocked by SSRF policy: %w", err)
			}
			return nil
		},
	}
}
