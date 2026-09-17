package tools

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/workspace"
)

func TestValidateURLForSSRF_RestrictedIPsAndHostnames(t *testing.T) {
	blockedURLs := []struct {
		url    string
		reason string
	}{
		// IPv4 Loopback
		{"http://127.0.0.1", "loopback"},
		{"http://127.0.0.1:8080/api", "loopback with port"},
		{"http://127.1.2.3:3000", "loopback block 127.0.0.0/8"},

		// Hostnames resolving to loopback / local
		{"http://localhost", "localhost"},
		{"http://localhost:8080/metrics", "localhost with port"},
		{"http://sub.localhost:8080", "sub.localhost"},
		{"http://service.local:5000", ".local hostname"},
		{"http://service.internal", ".internal hostname"},

		// Cloud Metadata (AWS, GCP, Azure, Alibaba)
		{"http://169.254.169.254/latest/meta-data/", "AWS/Azure metadata"},
		{"http://169.254.169.253", "AWS link-local DNS"},
		{"http://metadata.google.internal/computeMetadata/v1/", "GCP metadata"},
		{"http://metadata.internal", "generic metadata.internal"},
		{"http://instance-data/latest/meta-data/", "EC2 instance-data hostname"},
		{"http://100.100.100.200/latest/meta-data/", "Alibaba metadata"},

		// RFC 1918 Private ranges
		{"http://10.0.0.1", "10.0.0.0/8"},
		{"http://10.254.10.5:8080", "10.0.0.0/8 with port"},
		{"http://172.16.0.1", "172.16.0.0/12 start"},
		{"http://172.31.255.255", "172.16.0.0/12 end"},
		{"http://192.168.1.1", "192.168.0.0/16"},
		{"http://192.168.100.50:8443", "192.168.0.0/16 with port"},

		// CGNAT / Unspecified / Broadcast
		{"http://0.0.0.0", "unspecified 0.0.0.0"},
		{"http://0.0.0.0:8000", "unspecified with port"},
		{"http://100.64.0.1", "CGNAT 100.64.0.0/10"},
		{"http://255.255.255.255", "broadcast"},

		// IPv6 Loopback, Link-Local, Unique Local
		{"http://[::1]", "IPv6 loopback"},
		{"http://[::1]:8080", "IPv6 loopback with port"},
		{"http://[::]", "IPv6 unspecified"},
		{"http://[fe80::1]", "IPv6 link-local"},
		{"http://[fc00::1]", "IPv6 unique local"},
		{"http://[::ffff:127.0.0.1]", "IPv4-mapped IPv6 loopback"},
		{"http://[::ffff:169.254.169.254]", "IPv4-mapped metadata"},

		// Invalid Schemes
		{"ftp://example.com/file", "ftp scheme"},
		{"file:///etc/passwd", "file scheme"},
		{"gopher://127.0.0.1", "gopher scheme"},
		{"javascript:alert(1)", "javascript pseudo-scheme"},

		// Credentials in URL
		{"http://user:password@example.com", "userinfo in URL"},
	}

	for _, tc := range blockedURLs {
		t.Run(tc.reason, func(t *testing.T) {
			_, err := validateURLForSSRF(tc.url)
			if err == nil {
				t.Fatalf("expected URL %q (%s) to be rejected by SSRF policy, but got nil error", tc.url, tc.reason)
			}
		})
	}
}

func TestValidateURLForSSRF_AllowedPublicURLs(t *testing.T) {
	allowedURLs := []string{
		"https://example.com",
		"https://example.com/docs/api?query=hello",
		"http://example.com:8080/test",
		"https://api.github.com/repos/divmora/localharness",
		"https://golang.org/pkg/net/http/",
	}

	for _, u := range allowedURLs {
		t.Run(u, func(t *testing.T) {
			parsed, err := validateURLForSSRF(u)
			if err != nil {
				t.Fatalf("expected URL %q to be allowed, but got error: %v", u, err)
			}
			if parsed == nil {
				t.Fatalf("expected non-nil parsed URL for %q", u)
			}
		})
	}
}

func TestIsRestrictedIP(t *testing.T) {
	tests := []struct {
		ip         string
		restricted bool
	}{
		{"127.0.0.1", true},
		{"127.255.255.254", true},
		{"10.0.0.5", true},
		{"172.20.1.1", true},
		{"192.168.0.1", true},
		{"169.254.169.254", true},
		{"169.254.1.1", true},
		{"100.100.100.200", true},
		{"100.64.0.1", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"fe80::1", true},
		{"fc00::1", true},
		{"::ffff:127.0.0.1", true},
		{"::ffff:10.0.0.1", true},

		// Public IPs
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"142.250.190.46", false},
		{"2607:f8b0:4005:805::200e", false},
	}

	for _, tc := range tests {
		t.Run(tc.ip, func(t *testing.T) {
			parsed := net.ParseIP(tc.ip)
			if parsed == nil {
				t.Fatalf("failed to parse IP: %s", tc.ip)
			}
			got := isRestrictedIP(parsed)
			if got != tc.restricted {
				t.Errorf("isRestrictedIP(%s) = %v; want %v", tc.ip, got, tc.restricted)
			}
		})
	}
}

func TestSafeHTTPClient_BlocksRedirectToRestrictedTarget(t *testing.T) {
	// Start a test server that attempts to redirect to AWS metadata
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer ts.Close()

	client := newSafeHTTPClient()
	_, err := client.Get(ts.URL)
	if err == nil {
		t.Fatal("expected GET request with redirect to metadata to fail, got nil")
	}

	if !strings.Contains(err.Error(), "blocked by SSRF policy") && !strings.Contains(err.Error(), "restricted") {
		t.Errorf("expected error to mention SSRF/restricted policy, got: %v", err)
	}
}

func TestExecuteWebFetch_SSRFIntegration(t *testing.T) {
	wsMgr, _ := workspace.NewManager([]string{t.TempDir()})
	r := NewRegistry(wsMgr, slog.Default())
	registerWebFetch(r)

	// Attempt fetching cloud metadata
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReadUrlContent{
			ReadUrlContent: &pb.ActionReadUrlContent{
				Url: "http://169.254.169.254/latest/meta-data/",
			},
		},
	}

	err := r.Execute(context.Background(), "read_url_content", step)
	if err == nil {
		t.Fatal("expected read_url_content to return error for metadata URL, got nil")
	}

	if !strings.Contains(err.Error(), "blocked by security policy") && !strings.Contains(err.Error(), "restricted") {
		t.Errorf("expected security policy error, got: %v", err)
	}

	// Attempt fetching loopback
	stepLoopback := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReadUrlContent{
			ReadUrlContent: &pb.ActionReadUrlContent{
				Url: "http://127.0.0.1:8080/admin",
			},
		},
	}

	err = r.Execute(context.Background(), "read_url_content", stepLoopback)
	if err == nil {
		t.Fatal("expected read_url_content to return error for loopback URL, got nil")
	}
}
