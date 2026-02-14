package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackNotifier sends notifications formatted as Slack Block Kit messages.
type SlackNotifier struct {
	webhookURL   string
	client       *http.Client
	allowPrivate bool
}

// NewSlackNotifier creates a Slack notifier that sends Block Kit payloads.
func NewSlackNotifier(webhookURL string) *SlackNotifier {
	s := &SlackNotifier{
		webhookURL: webhookURL,
	}
	s.client = newSSRFSafeClient(&s.allowPrivate)
	return s
}

func (s *SlackNotifier) Name() string { return "slack" }

func (s *SlackNotifier) Send(ctx context.Context, notification Notification) error {
	if err := validateWebhookURL(s.webhookURL); err != nil {
		return fmt.Errorf("SSRF protection: %w", err)
	}

	payload := s.buildPayload(notification)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", contentTypeJSON)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send slack notification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}

	return nil
}

func (s *SlackNotifier) buildPayload(n Notification) map[string]any {
	// Determine status emoji
	var statusEmoji string
	switch {
	case n.CriticalCount > 0:
		statusEmoji = "\U0001f534"
	case n.WarningCount > 0:
		statusEmoji = "\U0001f7e1"
	default:
		statusEmoji = "\U0001f7e2"
	}

	return map[string]any{
		"blocks": []map[string]any{
			{
				"type": "header",
				"text": map[string]any{
					"type": "plain_text",
					"text": fmt.Sprintf("%s KubeAssist Health Report", statusEmoji),
				},
			},
			{
				"type": "section",
				"text": map[string]any{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*%s* in `%s`", n.RequestName, n.Namespace),
				},
			},
			{
				"type": "section",
				"fields": []map[string]any{
					{"type": "mrkdwn", "text": fmt.Sprintf("*Health Score*\n%.1f%%", n.HealthScore)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Issues*\n%d total", n.TotalIssues)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Critical*\n%d", n.CriticalCount)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Warning*\n%d", n.WarningCount)},
				},
			},
			{
				"type": "context",
				"elements": []map[string]any{
					{"type": "mrkdwn", "text": fmt.Sprintf("Checked at %s", n.Timestamp.UTC().Format(time.RFC3339))},
				},
			},
		},
	}
}
