package notifier

import (
	"context"
	"time"
)

// Notification represents a health check notification to send
type Notification struct {
	// Summary is a brief overview of the health check results
	Summary string `json:"summary"`
	// TotalHealthy is the count of healthy resources
	TotalHealthy int `json:"totalHealthy"`
	// TotalIssues is the count of issues found
	TotalIssues int `json:"totalIssues"`
	// CriticalCount is the count of critical issues
	CriticalCount int `json:"criticalCount"`
	// WarningCount is the count of warning issues
	WarningCount int `json:"warningCount"`
	// HealthScore is the computed health score (0-100)
	HealthScore float64 `json:"healthScore"`
	// Timestamp is when the check was performed
	Timestamp time.Time `json:"timestamp"`
	// RequestName is the name of the TeamHealthRequest that triggered this
	RequestName string `json:"requestName"`
	// Namespace is the namespace of the TeamHealthRequest
	Namespace string `json:"namespace"`
}

// Notifier is the interface for sending health check notifications
type Notifier interface {
	// Name returns the notifier identifier
	Name() string
	// Send dispatches a notification
	Send(ctx context.Context, notification Notification) error
}
