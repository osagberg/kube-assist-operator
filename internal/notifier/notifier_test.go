package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebhookNotifier_Send(t *testing.T) {
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
		{
			name:       "accepted status returns no error",
			statusCode: http.StatusAccepted,
			wantErr:    false,
		},
		{
			name:       "bad request returns error",
			statusCode: http.StatusBadRequest,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			wh := NewWebhookNotifier(srv.URL)
			wh.allowPrivate = true // httptest binds to 127.0.0.1
			err := wh.Send(context.Background(), Notification{
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

func TestWebhookNotifier_SendValidatesJSONBody(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
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

	wh := NewWebhookNotifier(srv.URL)
	wh.allowPrivate = true // httptest binds to 127.0.0.1
	if err := wh.Send(context.Background(), notification); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	var got Notification
	if err := json.Unmarshal(receivedBody, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	if got.Summary != notification.Summary {
		t.Errorf("Summary = %q, want %q", got.Summary, notification.Summary)
	}
	if got.TotalHealthy != notification.TotalHealthy {
		t.Errorf("TotalHealthy = %d, want %d", got.TotalHealthy, notification.TotalHealthy)
	}
	if got.TotalIssues != notification.TotalIssues {
		t.Errorf("TotalIssues = %d, want %d", got.TotalIssues, notification.TotalIssues)
	}
	if got.CriticalCount != notification.CriticalCount {
		t.Errorf("CriticalCount = %d, want %d", got.CriticalCount, notification.CriticalCount)
	}
	if got.WarningCount != notification.WarningCount {
		t.Errorf("WarningCount = %d, want %d", got.WarningCount, notification.WarningCount)
	}
	if got.HealthScore != notification.HealthScore {
		t.Errorf("HealthScore = %f, want %f", got.HealthScore, notification.HealthScore)
	}
	if got.RequestName != notification.RequestName {
		t.Errorf("RequestName = %q, want %q", got.RequestName, notification.RequestName)
	}
	if got.Namespace != notification.Namespace {
		t.Errorf("Namespace = %q, want %q", got.Namespace, notification.Namespace)
	}
}

func TestWebhookNotifier_Name(t *testing.T) {
	wh := NewWebhookNotifier("http://example.com")
	if got := wh.Name(); got != "webhook" {
		t.Errorf("Name() = %q, want %q", got, "webhook")
	}
}

func TestNoopNotifier_CapturesNotifications(t *testing.T) {
	n := NewNoopNotifier()

	notifications := []Notification{
		{Summary: "first", TotalHealthy: 1},
		{Summary: "second", TotalHealthy: 2},
		{Summary: "third", TotalHealthy: 3},
	}

	for _, notif := range notifications {
		if err := n.Send(context.Background(), notif); err != nil {
			t.Fatalf("Send() unexpected error: %v", err)
		}
	}

	if len(n.Sent) != len(notifications) {
		t.Fatalf("Sent count = %d, want %d", len(n.Sent), len(notifications))
	}

	for i, notif := range notifications {
		if n.Sent[i].Summary != notif.Summary {
			t.Errorf("Sent[%d].Summary = %q, want %q", i, n.Sent[i].Summary, notif.Summary)
		}
		if n.Sent[i].TotalHealthy != notif.TotalHealthy {
			t.Errorf("Sent[%d].TotalHealthy = %d, want %d", i, n.Sent[i].TotalHealthy, notif.TotalHealthy)
		}
	}
}

func TestNoopNotifier_Name(t *testing.T) {
	n := NewNoopNotifier()
	if got := n.Name(); got != "noop" {
		t.Errorf("Name() = %q, want %q", got, "noop")
	}
}

// errorNotifier is a test helper that always returns an error
type errorNotifier struct{}

func (e *errorNotifier) Name() string { return "error" }
func (e *errorNotifier) Send(_ context.Context, _ Notification) error {
	return fmt.Errorf("always fails")
}

func TestRegistry_NotifyAll(t *testing.T) {
	tests := []struct {
		name      string
		notifiers []Notifier
		wantErr   bool
		wantSent  int // expected count on noop notifiers
	}{
		{
			name:      "sends to all notifiers",
			notifiers: []Notifier{NewNoopNotifier(), NewNoopNotifier()},
			wantErr:   false,
			wantSent:  1,
		},
		{
			name:      "continues after one notifier fails",
			notifiers: []Notifier{&errorNotifier{}, NewNoopNotifier()},
			wantErr:   true,
			wantSent:  1,
		},
		{
			name:      "empty registry returns no error",
			notifiers: []Notifier{},
			wantErr:   false,
			wantSent:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewRegistry()
			for _, n := range tt.notifiers {
				reg.Register(n)
			}

			err := reg.NotifyAll(context.Background(), Notification{Summary: "test"})
			if (err != nil) != tt.wantErr {
				t.Errorf("NotifyAll() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Verify noop notifiers received the notification
			for _, n := range tt.notifiers {
				if noop, ok := n.(*NoopNotifier); ok {
					if len(noop.Sent) != tt.wantSent {
						t.Errorf("NoopNotifier.Sent count = %d, want %d", len(noop.Sent), tt.wantSent)
					}
				}
			}
		})
	}
}

func TestWebhookNotifier_SSRFProtection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := NewWebhookNotifier(srv.URL)
	// allowPrivate defaults to false — SSRF check blocks loopback
	err := wh.Send(context.Background(), Notification{Summary: "test"})
	if err == nil {
		t.Fatal("expected SSRF error for loopback address, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "SSRF protection") {
		t.Errorf("error = %q, want to contain 'SSRF protection'", got)
	}
}

func TestValidateWebhookTarget_SchemeAndHostValidation(t *testing.T) {
	t.Run("rejects non-http scheme", func(t *testing.T) {
		err := validateWebhookTarget("file:///tmp/test.sock")
		if err == nil {
			t.Fatal("expected error for invalid scheme, got nil")
		}
		if got := err.Error(); !strings.Contains(got, "http or https") {
			t.Errorf("error = %q, want to contain scheme validation message", got)
		}
	})

	t.Run("rejects missing host", func(t *testing.T) {
		err := validateWebhookTarget("https:///path-only")
		if err == nil {
			t.Fatal("expected error for missing host, got nil")
		}
		if got := err.Error(); !strings.Contains(got, "include host") {
			t.Errorf("error = %q, want to contain host validation message", got)
		}
	})
}

func TestRegistry_Len(t *testing.T) {
	reg := NewRegistry()
	if reg.Len() != 0 {
		t.Errorf("Len() = %d, want 0", reg.Len())
	}

	reg.Register(NewNoopNotifier())
	if reg.Len() != 1 {
		t.Errorf("Len() = %d, want 1", reg.Len())
	}

	reg.Register(NewNoopNotifier())
	if reg.Len() != 2 {
		t.Errorf("Len() = %d, want 2", reg.Len())
	}
}
