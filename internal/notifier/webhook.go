package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const contentTypeJSON = "application/json"

// SECURITY-011: The webhook URL annotation on TeamHealthRequest CRs is user-controlled.
// Any user with CR create permissions can set the HTTP webhook annotation, which means
// they control the destination of outbound HTTP calls from the operator.
// Consider restricting webhook annotation usage to cluster administrators only in future.

// WebhookNotifier sends notifications via HTTP POST to a webhook URL.
// Compatible with Slack, Mattermost, Discord, and any HTTP endpoint.
type WebhookNotifier struct {
	url          string
	client       *http.Client
	allowPrivate bool // skip SSRF check (testing only)
}

// NewWebhookNotifier creates a new webhook notifier
func NewWebhookNotifier(webhookURL string) *WebhookNotifier {
	w := &WebhookNotifier{
		url: webhookURL,
	}
	w.client = newSSRFSafeClient(&w.allowPrivate)
	return w
}

func (w *WebhookNotifier) Name() string {
	return "webhook"
}

// privateNetworks contains CIDR ranges for internal/private networks.
var privateNetworks = []net.IPNet{
	{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
	{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
	{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
	{IP: net.IPv4(169, 254, 0, 0), Mask: net.CIDRMask(16, 32)},
	{IP: net.IPv4(127, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
	{IP: net.IPv6loopback, Mask: net.CIDRMask(128, 128)},
	{IP: net.IPv6unspecified, Mask: net.CIDRMask(128, 128)},
	{IP: net.ParseIP("fe80::"), Mask: net.CIDRMask(10, 128)},
	{IP: net.ParseIP("fc00::"), Mask: net.CIDRMask(7, 128)},
}

// isPrivateIP reports whether ip falls within any private/reserved network range.
func isPrivateIP(ip net.IP) bool {
	for _, cidr := range privateNetworks {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// newSafeDialContext returns a DialContext function that resolves the host and validates
// all resolved IPs against private network ranges before connecting. This makes the
// SSRF check atomic, eliminating the TOCTOU window between DNS lookup and connection.
// The allowPrivate pointer is checked at dial time so tests can toggle it after construction.
func newSafeDialContext(allowPrivate *bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q: %w", addr, err)
		}

		// If private IPs are allowed (testing), use the default dialer directly.
		if allowPrivate != nil && *allowPrivate {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}

		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("DNS lookup failed for %s: %w", host, err)
		}

		// Validate ALL resolved IPs before connecting to any.
		for _, ipAddr := range ips {
			if isPrivateIP(ipAddr.IP) {
				return nil, fmt.Errorf("SSRF protection: %s resolves to private IP %s", host, ipAddr.IP)
			}
		}

		// Connect to the first resolved IP (they all passed validation).
		var d net.Dialer
		for _, ipAddr := range ips {
			target := net.JoinHostPort(ipAddr.IP.String(), port)
			conn, err := d.DialContext(ctx, network, target)
			if err == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("failed to connect to %s: all resolved IPs unreachable", host)
	}
}

// newSSRFSafeClient creates an http.Client with SSRF protection via a safe dial context.
// The allowPrivate pointer is checked at dial time so tests can toggle it after construction.
func newSSRFSafeClient(allowPrivate *bool) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: newSafeDialContext(allowPrivate),
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// validateWebhookTarget validates a webhook URL and checks that the host does
// not resolve to private IP ranges. Retained for test compatibility and as
// a defense-in-depth check alongside the transport-level safeDialContext.
func validateWebhookTarget(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook URL must use http or https scheme")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("webhook URL must include host")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed for %s: %w", host, err)
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("webhook target %s resolves to private IP %s", host, ip)
		}
	}
	return nil
}

// validateWebhookURL performs a basic URL validation (scheme and host).
// The actual IP-level SSRF check is done atomically by the transport's safeDialContext.
func validateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook URL must use http or https scheme")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("webhook URL must include host")
	}
	return nil
}

func (w *WebhookNotifier) Send(ctx context.Context, notification Notification) error {
	if err := validateWebhookURL(w.url); err != nil {
		return fmt.Errorf("SSRF protection: %w", err)
	}

	body, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", contentTypeJSON)

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}
