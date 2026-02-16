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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEventTimestamp(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	earlier := now.Add(-5 * time.Minute)
	earliest := now.Add(-10 * time.Minute)

	tests := []struct {
		name string
		ev   *corev1.Event
		want time.Time
	}{
		{
			name: "LastTimestamp preferred",
			ev: &corev1.Event{
				LastTimestamp: metav1.NewTime(now),
				EventTime:     metav1.NewMicroTime(earlier),
				ObjectMeta:    metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(earliest)},
			},
			want: now,
		},
		{
			name: "EventTime fallback when LastTimestamp zero",
			ev: &corev1.Event{
				EventTime:  metav1.NewMicroTime(earlier),
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(earliest)},
			},
			want: earlier,
		},
		{
			name: "CreationTimestamp fallback when both zero",
			ev: &corev1.Event{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(earliest)},
			},
			want: earliest,
		},
		{
			name: "all zero returns zero time",
			ev:   &corev1.Event{},
			want: time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EventTimestamp(tt.ev)
			if !got.Equal(tt.want) {
				t.Errorf("EventTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}
