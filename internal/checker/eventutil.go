/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package checker

import (
	"time"

	corev1 "k8s.io/api/core/v1"
)

// TODO(agent-3): use checker.EventTimestamp in troubleshootrequest_controller.go
// to eliminate the duplicated event-timestamp extraction logic.

// EventTimestamp returns the best available timestamp for a Kubernetes event.
// It checks LastTimestamp, EventTime, and CreationTimestamp in that order.
func EventTimestamp(ev *corev1.Event) time.Time {
	if !ev.LastTimestamp.IsZero() {
		return ev.LastTimestamp.Time
	}
	if !ev.EventTime.Time.IsZero() {
		return ev.EventTime.Time
	}
	return ev.CreationTimestamp.Time
}
