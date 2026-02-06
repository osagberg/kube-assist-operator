package notifier

import "context"

// NoopNotifier is a no-op notifier for testing
type NoopNotifier struct {
	Sent []Notification
}

func NewNoopNotifier() *NoopNotifier {
	return &NoopNotifier{}
}

func (n *NoopNotifier) Name() string {
	return "noop"
}

func (n *NoopNotifier) Send(_ context.Context, notification Notification) error {
	n.Sent = append(n.Sent, notification)
	return nil
}
