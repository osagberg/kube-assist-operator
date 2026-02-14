package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTeamsNotifier_Send(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "successful POST returns no error",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "server error returns error",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			tm := NewTeamsNotifier(srv.URL)
			tm.allowPrivate = true
			err := tm.Send(context.Background(), Notification{
				Summary:      "test",
				TotalHealthy: 5,
				TotalIssues:  2,
			})

			if (err != nil) != tt.wantErr {
				t.Errorf("Send() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTeamsNotifier_PayloadStructure(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if ct := r.Header.Get("Content-Type"); ct != contentTypeJSON {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	notification := Notification{
		Summary:       "3 issues found",
		TotalHealthy:  10,
		TotalIssues:   3,
		CriticalCount: 1,
		WarningCount:  2,
		HealthScore:   76.9,
		Timestamp:     ts,
		RequestName:   "my-check",
		Namespace:     "default",
	}

	tm := NewTeamsNotifier(srv.URL)
	tm.allowPrivate = true
	if err := tm.Send(context.Background(), notification); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(receivedBody, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	if got["type"] != "message" {
		t.Errorf("type = %v, want message", got["type"])
	}

	attachments := got["attachments"].([]any)
	if len(attachments) != 1 {
		t.Fatalf("attachments length = %d, want 1", len(attachments))
	}

	attachment := attachments[0].(map[string]any)
	if attachment["contentType"] != "application/vnd.microsoft.card.adaptive" {
		t.Errorf("contentType = %v, want application/vnd.microsoft.card.adaptive", attachment["contentType"])
	}

	content := attachment["content"].(map[string]any)
	if content["type"] != "AdaptiveCard" {
		t.Errorf("content type = %v, want AdaptiveCard", content["type"])
	}
	if content["version"] != "1.4" {
		t.Errorf("version = %v, want 1.4", content["version"])
	}
	if content["$schema"] != "http://adaptivecards.io/schemas/adaptive-card.json" {
		t.Errorf("schema = %v, want adaptivecards schema", content["$schema"])
	}

	body := content["body"].([]any)
	if len(body) != 4 {
		t.Errorf("body length = %d, want 4", len(body))
	}

	// Verify header
	headerBlock := body[0].(map[string]any)
	if headerBlock["text"] != "KubeAssist Health Report" {
		t.Errorf("header text = %v, want 'KubeAssist Health Report'", headerBlock["text"])
	}

	// Verify subtitle
	subtitle := body[1].(map[string]any)
	if subtitle["text"] != "my-check in default" {
		t.Errorf("subtitle text = %v, want 'my-check in default'", subtitle["text"])
	}
}

func TestTeamsNotifier_FactSet(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	notification := Notification{
		Summary:       "test",
		TotalHealthy:  10,
		TotalIssues:   3,
		CriticalCount: 1,
		WarningCount:  2,
		HealthScore:   76.9,
		Timestamp:     time.Now(),
		RequestName:   "my-check",
		Namespace:     "default",
	}

	tm := NewTeamsNotifier(srv.URL)
	tm.allowPrivate = true
	if err := tm.Send(context.Background(), notification); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(receivedBody, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	attachments := got["attachments"].([]any)
	content := attachments[0].(map[string]any)["content"].(map[string]any)
	body := content["body"].([]any)

	// FactSet is the 4th element (index 3)
	factSet := body[3].(map[string]any)
	if factSet["type"] != "FactSet" {
		t.Errorf("factSet type = %v, want FactSet", factSet["type"])
	}

	facts := factSet["facts"].([]any)
	if len(facts) != 3 {
		t.Fatalf("facts length = %d, want 3", len(facts))
	}

	expectedFacts := []struct {
		title string
		value string
	}{
		{"Critical", "1"},
		{"Warning", "2"},
		{"Healthy", "10"},
	}

	for i, expected := range expectedFacts {
		fact := facts[i].(map[string]any)
		if fact["title"] != expected.title {
			t.Errorf("fact[%d].title = %v, want %v", i, fact["title"], expected.title)
		}
		if fact["value"] != expected.value {
			t.Errorf("fact[%d].value = %v, want %v", i, fact["value"], expected.value)
		}
	}
}

func TestTeamsNotifier_Name(t *testing.T) {
	tm := NewTeamsNotifier("http://example.com")
	if got := tm.Name(); got != "teams" {
		t.Errorf("Name() = %q, want %q", got, "teams")
	}
}

func TestTeamsNotifier_SSRFProtection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tm := NewTeamsNotifier(srv.URL)
	// allowPrivate defaults to false — SSRF check blocks loopback
	err := tm.Send(context.Background(), Notification{Summary: "test"})
	if err == nil {
		t.Fatal("expected SSRF error for loopback address, got nil")
	}
	if !strings.Contains(err.Error(), "SSRF protection") {
		t.Errorf("error = %q, want to contain 'SSRF protection'", err.Error())
	}
}
