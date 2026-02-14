package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const pagerDutyEventsURL = "https://events.pagerduty.com/v2/enqueue"

// PagerDutyNotifier sends notifications via PagerDuty Events API v2.
type PagerDutyNotifier struct {
	routingKey   string
	apiURL       string
	client       *http.Client
	allowPrivate bool
}

// NewPagerDutyNotifier creates a PagerDuty notifier. The routingKey is the
// PagerDuty integration key (passed via the URL field in NotificationTarget).
func NewPagerDutyNotifier(routingKey string) *PagerDutyNotifier {
	return &PagerDutyNotifier{
		routingKey: routingKey,
		apiURL:     pagerDutyEventsURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (p *PagerDutyNotifier) Name() string { return "pagerduty" }

func (p *PagerDutyNotifier) Send(ctx context.Context, notification Notification) error {
	if !p.allowPrivate {
		if err := validateWebhookTarget(p.apiURL); err != nil {
			return fmt.Errorf("SSRF protection: %w", err)
		}
	}

	payload := p.buildPayload(notification)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal pagerduty payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", contentTypeJSON)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send pagerduty notification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pagerduty returned status %d", resp.StatusCode)
	}

	return nil
}

func (p *PagerDutyNotifier) buildPayload(n Notification) map[string]any {
	dedupKey := fmt.Sprintf("kubeassist/%s/%s", n.Namespace, n.RequestName)

	// Auto-resolve when no issues
	eventAction := "trigger"
	if n.TotalIssues == 0 {
		eventAction = "resolve"
	}

	severity := p.mapSeverity(n)

	return map[string]any{
		"routing_key":  p.routingKey,
		"dedup_key":    dedupKey,
		"event_action": eventAction,
		"payload": map[string]any{
			"summary":   n.Summary,
			"source":    "kubeassist",
			"severity":  severity,
			"timestamp": n.Timestamp.UTC().Format(time.RFC3339),
			"custom_details": map[string]any{
				"health_score":   n.HealthScore,
				"total_healthy":  n.TotalHealthy,
				"total_issues":   n.TotalIssues,
				"critical_count": n.CriticalCount,
				"warning_count":  n.WarningCount,
				"request_name":   n.RequestName,
				"namespace":      n.Namespace,
			},
		},
	}
}

func (p *PagerDutyNotifier) mapSeverity(n Notification) string {
	switch {
	case n.CriticalCount > 0:
		return "critical"
	case n.WarningCount > 0:
		return "warning"
	default:
		return "info"
	}
}
