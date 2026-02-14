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

func TestPagerDutyNotifier_Send(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "successful POST returns no error",
			statusCode: http.StatusAccepted,
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

			p := NewPagerDutyNotifier("test-routing-key")
			p.apiURL = srv.URL
			p.allowPrivate = true
			err := p.Send(context.Background(), Notification{
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

func TestPagerDutyNotifier_PayloadStructure(t *testing.T) {
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
		w.WriteHeader(http.StatusAccepted)
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

	p := NewPagerDutyNotifier("test-routing-key")
	p.apiURL = srv.URL
	p.allowPrivate = true
	if err := p.Send(context.Background(), notification); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(receivedBody, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	if got["routing_key"] != "test-routing-key" {
		t.Errorf("routing_key = %v, want test-routing-key", got["routing_key"])
	}
	if got["dedup_key"] != "kubeassist/default/my-check" {
		t.Errorf("dedup_key = %v, want kubeassist/default/my-check", got["dedup_key"])
	}
	if got["event_action"] != "trigger" {
		t.Errorf("event_action = %v, want trigger", got["event_action"])
	}

	payload := got["payload"].(map[string]any)
	if payload["summary"] != "3 issues found" {
		t.Errorf("summary = %v, want '3 issues found'", payload["summary"])
	}
	if payload["source"] != "kubeassist" {
		t.Errorf("source = %v, want kubeassist", payload["source"])
	}
	if payload["severity"] != "critical" {
		t.Errorf("severity = %v, want critical", payload["severity"])
	}

	details := payload["custom_details"].(map[string]any)
	if details["request_name"] != "my-check" {
		t.Errorf("request_name = %v, want my-check", details["request_name"])
	}
	if details["namespace"] != "default" {
		t.Errorf("namespace = %v, want default", details["namespace"])
	}
}

func TestPagerDutyNotifier_SeverityMapping(t *testing.T) {
	tests := []struct {
		name          string
		criticalCount int
		warningCount  int
		wantSeverity  string
	}{
		{
			name:          "critical issues map to critical",
			criticalCount: 1,
			warningCount:  0,
			wantSeverity:  "critical",
		},
		{
			name:          "warning issues map to warning",
			criticalCount: 0,
			warningCount:  2,
			wantSeverity:  "warning",
		},
		{
			name:          "no issues maps to info",
			criticalCount: 0,
			warningCount:  0,
			wantSeverity:  "info",
		},
		{
			name:          "both critical and warning maps to critical",
			criticalCount: 1,
			warningCount:  2,
			wantSeverity:  "critical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var err error
				receivedBody, err = io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				w.WriteHeader(http.StatusAccepted)
			}))
			defer srv.Close()

			p := NewPagerDutyNotifier("test-key")
			p.apiURL = srv.URL
			p.allowPrivate = true
			err := p.Send(context.Background(), Notification{
				Summary:       "test",
				TotalIssues:   tt.criticalCount + tt.warningCount,
				CriticalCount: tt.criticalCount,
				WarningCount:  tt.warningCount,
			})
			if err != nil {
				t.Fatalf("Send() unexpected error: %v", err)
			}

			var got map[string]any
			if err := json.Unmarshal(receivedBody, &got); err != nil {
				t.Fatalf("unmarshal body: %v", err)
			}

			payload := got["payload"].(map[string]any)
			if payload["severity"] != tt.wantSeverity {
				t.Errorf("severity = %v, want %v", payload["severity"], tt.wantSeverity)
			}
		})
	}
}

func TestPagerDutyNotifier_AutoResolve(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPagerDutyNotifier("test-key")
	p.apiURL = srv.URL
	p.allowPrivate = true
	err := p.Send(context.Background(), Notification{
		Summary:      "All healthy",
		TotalHealthy: 10,
		TotalIssues:  0,
		RequestName:  "my-check",
		Namespace:    "default",
	})
	if err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(receivedBody, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	if got["event_action"] != "resolve" {
		t.Errorf("event_action = %v, want resolve", got["event_action"])
	}
	if got["dedup_key"] != "kubeassist/default/my-check" {
		t.Errorf("dedup_key = %v, want kubeassist/default/my-check", got["dedup_key"])
	}
}

func TestPagerDutyNotifier_Name(t *testing.T) {
	p := NewPagerDutyNotifier("test-key")
	if got := p.Name(); got != "pagerduty" {
		t.Errorf("Name() = %q, want %q", got, "pagerduty")
	}
}

func TestPagerDutyNotifier_SSRFProtection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPagerDutyNotifier("test-key")
	p.apiURL = srv.URL
	// allowPrivate defaults to false — SSRF check blocks loopback
	err := p.Send(context.Background(), Notification{Summary: "test"})
	if err == nil {
		t.Fatal("expected SSRF error for loopback address, got nil")
	}
	if !strings.Contains(err.Error(), "SSRF protection") {
		t.Errorf("error = %q, want to contain 'SSRF protection'", err.Error())
	}
}
