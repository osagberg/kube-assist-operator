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

func TestSlackNotifier_Send(t *testing.T) {
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

			s := NewSlackNotifier(srv.URL)
			s.allowPrivate = true
			err := s.Send(context.Background(), Notification{
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

func TestSlackNotifier_PayloadStructure(t *testing.T) {
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

	s := NewSlackNotifier(srv.URL)
	s.allowPrivate = true
	if err := s.Send(context.Background(), notification); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(receivedBody, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	blocks, ok := got["blocks"].([]any)
	if !ok {
		t.Fatal("payload missing 'blocks' array")
	}
	if len(blocks) != 4 {
		t.Errorf("blocks length = %d, want 4", len(blocks))
	}

	// Verify header block
	header := blocks[0].(map[string]any)
	if header["type"] != "header" {
		t.Errorf("first block type = %v, want header", header["type"])
	}

	// Verify section with request name
	section := blocks[1].(map[string]any)
	if section["type"] != "section" {
		t.Errorf("second block type = %v, want section", section["type"])
	}
	sectionText := section["text"].(map[string]any)
	text := sectionText["text"].(string)
	if text != "*my-check* in `default`" {
		t.Errorf("section text = %q, want '*my-check* in `default`'", text)
	}

	// Verify fields section
	fieldsSection := blocks[2].(map[string]any)
	fields := fieldsSection["fields"].([]any)
	if len(fields) != 4 {
		t.Errorf("fields length = %d, want 4", len(fields))
	}

	// Verify context block
	ctxBlock := blocks[3].(map[string]any)
	if ctxBlock["type"] != "context" {
		t.Errorf("fourth block type = %v, want context", ctxBlock["type"])
	}
}

func TestSlackNotifier_EmptyIssues(t *testing.T) {
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
		Summary:       "All healthy",
		TotalHealthy:  10,
		TotalIssues:   0,
		CriticalCount: 0,
		WarningCount:  0,
		HealthScore:   100.0,
		Timestamp:     time.Now(),
		RequestName:   "my-check",
		Namespace:     "default",
	}

	s := NewSlackNotifier(srv.URL)
	s.allowPrivate = true
	if err := s.Send(context.Background(), notification); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(receivedBody, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	// Verify green emoji in header for healthy state
	blocks := got["blocks"].([]any)
	header := blocks[0].(map[string]any)
	headerText := header["text"].(map[string]any)
	text := headerText["text"].(string)
	if text != "\U0001f7e2 KubeAssist Health Report" {
		t.Errorf("header text = %q, want green emoji for healthy", text)
	}
}

func TestSlackNotifier_Name(t *testing.T) {
	s := NewSlackNotifier("http://example.com")
	if got := s.Name(); got != "slack" {
		t.Errorf("Name() = %q, want %q", got, "slack")
	}
}

func TestSlackNotifier_SSRFProtection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := NewSlackNotifier(srv.URL)
	// allowPrivate defaults to false — SSRF check blocks loopback
	err := s.Send(context.Background(), Notification{Summary: "test"})
	if err == nil {
		t.Fatal("expected SSRF error for loopback address, got nil")
	}
	if !strings.Contains(err.Error(), "SSRF protection") {
		t.Errorf("error = %q, want to contain 'SSRF protection'", err.Error())
	}
}

func TestSlackNotifier_RedirectNotFollowed(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect was followed — request reached target server")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/webhook", http.StatusFound)
	}))
	defer redirector.Close()

	s := NewSlackNotifier(redirector.URL)
	s.allowPrivate = true
	err := s.Send(context.Background(), Notification{Summary: "test"})
	if err == nil {
		t.Fatal("expected error for redirect response, got nil")
	}
	if !strings.Contains(err.Error(), "status 302") {
		t.Errorf("error = %q, want to contain 'status 302'", err.Error())
	}
}

func TestSlackNotifier_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := NewSlackNotifier(srv.URL)
	s.allowPrivate = true

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := s.Send(ctx, Notification{Summary: "test"})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
