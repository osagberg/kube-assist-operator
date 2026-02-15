package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// TeamsNotifier sends notifications formatted as Microsoft Teams Adaptive Cards.
type TeamsNotifier struct {
	webhookURL   string
	client       *http.Client
	allowPrivate bool
}

// NewTeamsNotifier creates a Teams notifier that sends Adaptive Card payloads.
func NewTeamsNotifier(webhookURL string) *TeamsNotifier {
	t := &TeamsNotifier{
		webhookURL: webhookURL,
	}
	t.client = newSSRFSafeClient(&t.allowPrivate)
	return t
}

func (t *TeamsNotifier) Name() string { return "teams" }

func (t *TeamsNotifier) Send(ctx context.Context, notification Notification) error {
	if err := validateWebhookURL(t.webhookURL); err != nil {
		return fmt.Errorf("SSRF protection: %w", err)
	}

	payload := t.buildPayload(notification)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal teams payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", contentTypeJSON)

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("send teams notification: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("teams returned status %d", resp.StatusCode)
	}

	return nil
}

func (t *TeamsNotifier) buildPayload(n Notification) map[string]any {
	return map[string]any{
		"type": "message",
		"attachments": []map[string]any{
			{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content": map[string]any{
					"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
					"type":    "AdaptiveCard",
					"version": "1.4",
					"body": []map[string]any{
						{
							"type":   "TextBlock",
							"size":   "Large",
							"weight": "Bolder",
							"text":   "KubeAssist Health Report",
						},
						{
							"type":     "TextBlock",
							"text":     fmt.Sprintf("%s in %s", n.RequestName, n.Namespace),
							"isSubtle": true,
						},
						{
							"type": "ColumnSet",
							"columns": []map[string]any{
								{
									"type": "Column",
									"items": []map[string]any{
										{"type": "TextBlock", "text": "Health Score", "weight": "Bolder"},
										{"type": "TextBlock", "text": fmt.Sprintf("%.1f%%", n.HealthScore)},
									},
								},
								{
									"type": "Column",
									"items": []map[string]any{
										{"type": "TextBlock", "text": "Issues", "weight": "Bolder"},
										{"type": "TextBlock", "text": fmt.Sprintf("%d", n.TotalIssues)},
									},
								},
							},
						},
						{
							"type": "FactSet",
							"facts": []map[string]any{
								{"title": "Critical", "value": fmt.Sprintf("%d", n.CriticalCount)},
								{"title": "Warning", "value": fmt.Sprintf("%d", n.WarningCount)},
								{"title": "Healthy", "value": fmt.Sprintf("%d", n.TotalHealthy)},
							},
						},
					},
				},
			},
		},
	}
}
