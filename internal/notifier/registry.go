package notifier

import (
	"context"
	"fmt"
	"log/slog"
)

// Registry manages multiple notifiers
type Registry struct {
	notifiers []Notifier
}

// NewRegistry creates a new notifier registry
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a notifier
func (r *Registry) Register(n Notifier) {
	r.notifiers = append(r.notifiers, n)
}

// NotifyAll sends a notification through all registered notifiers.
// Errors are logged but do not stop other notifiers from firing.
func (r *Registry) NotifyAll(ctx context.Context, notification Notification) error {
	var firstErr error
	for _, n := range r.notifiers {
		if err := n.Send(ctx, notification); err != nil {
			slog.Error("notifier failed", "notifier", n.Name(), "error", err)
			if firstErr == nil {
				firstErr = fmt.Errorf("notifier %s: %w", n.Name(), err)
			}
		}
	}
	return firstErr
}

// Len returns the number of registered notifiers
func (r *Registry) Len() int {
	return len(r.notifiers)
}
