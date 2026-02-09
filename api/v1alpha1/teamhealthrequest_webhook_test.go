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

package v1alpha1

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestTeamHealthRequestWebhook_ValidCreate(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: TeamHealthRequestSpec{
			Scope: ScopeConfig{
				CurrentNamespaceOnly: true,
			},
			Checks: []CheckerName{CheckerWorkloads, CheckerSecrets},
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error: %v", err)
	}
}

func TestTeamHealthRequestWebhook_InvalidCheckerName(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		Spec: TeamHealthRequestSpec{
			Checks: []CheckerName{"invalid-checker"},
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err == nil {
		t.Error("ValidateCreate() expected error for invalid checker name")
	}
}

func TestTeamHealthRequestWebhook_NegativeTTL(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		Spec: TeamHealthRequestSpec{
			TTLSecondsAfterFinished: ptr.To(int32(-1)),
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err == nil {
		t.Error("ValidateCreate() expected error for negative TTL")
	}
}

func TestTeamHealthRequestWebhook_ValidTTL(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		Spec: TeamHealthRequestSpec{
			TTLSecondsAfterFinished: ptr.To(int32(120)),
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error: %v", err)
	}
}

func TestTeamHealthRequestWebhook_ConflictingScopeNamespaces(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		Spec: TeamHealthRequestSpec{
			Scope: ScopeConfig{
				CurrentNamespaceOnly: true,
				Namespaces:           []string{"ns1"},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err == nil {
		t.Error("ValidateCreate() expected error for conflicting scope (currentNamespaceOnly + namespaces)")
	}
}

func TestTeamHealthRequestWebhook_ConflictingScopeSelector(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		Spec: TeamHealthRequestSpec{
			Scope: ScopeConfig{
				CurrentNamespaceOnly: true,
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"team": "platform"},
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err == nil {
		t.Error("ValidateCreate() expected error for conflicting scope (currentNamespaceOnly + namespaceSelector)")
	}
}

func TestTeamHealthRequestWebhook_ImmutableScope(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	old := &TeamHealthRequest{
		Spec: TeamHealthRequestSpec{
			Scope: ScopeConfig{CurrentNamespaceOnly: true},
		},
	}
	updated := &TeamHealthRequest{
		Spec: TeamHealthRequestSpec{
			Scope: ScopeConfig{CurrentNamespaceOnly: false},
		},
	}
	_, err := v.ValidateUpdate(context.Background(), old, updated)
	if err == nil {
		t.Error("ValidateUpdate() expected error for scope change")
	}
}

func TestTeamHealthRequestWebhook_AllowSameSpecUpdate(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	old := &TeamHealthRequest{
		Spec: TeamHealthRequestSpec{
			Scope:  ScopeConfig{CurrentNamespaceOnly: true},
			Checks: []CheckerName{CheckerWorkloads},
		},
	}
	updated := &TeamHealthRequest{
		Spec: TeamHealthRequestSpec{
			Scope:  ScopeConfig{CurrentNamespaceOnly: true},
			Checks: []CheckerName{CheckerWorkloads},
		},
		Status: TeamHealthRequestStatus{
			Phase: TeamHealthPhaseCompleted,
		},
	}
	_, err := v.ValidateUpdate(context.Background(), old, updated)
	if err != nil {
		t.Errorf("ValidateUpdate() unexpected error: %v", err)
	}
}

func TestTeamHealthRequestWebhook_DeleteAlwaysAllowed(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	_, err := v.ValidateDelete(context.Background(), &TeamHealthRequest{})
	if err != nil {
		t.Errorf("ValidateDelete() unexpected error: %v", err)
	}
}

func TestTeamHealthRequestWebhook_AllValidCheckerNames(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	allCheckers := []CheckerName{
		CheckerWorkloads, CheckerHelmReleases, CheckerKustomizations,
		CheckerGitRepositories, CheckerSecrets, CheckerPVCs,
		CheckerQuotas, CheckerNetworkPolicies,
	}
	hr := &TeamHealthRequest{
		Spec: TeamHealthRequestSpec{
			Checks: allCheckers,
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error with all valid checkers: %v", err)
	}
}

func TestTeamHealthRequestWebhook_EmptyChecksAllowed(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		Spec: TeamHealthRequestSpec{},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error for empty checks (should default to all): %v", err)
	}
}

func TestTeamHealthRequestWebhook_ValidNotificationHTTPS(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test-notify-https", Namespace: "default"},
		Spec: TeamHealthRequestSpec{
			Notify: []NotificationTarget{
				{
					Type:         NotificationTypeWebhook,
					URL:          "https://example.com/webhook",
					OnCompletion: true,
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error for valid HTTPS notification: %v", err)
	}
}

func TestTeamHealthRequestWebhook_ValidNotificationSecretRef(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test-notify-secret", Namespace: "default"},
		Spec: TeamHealthRequestSpec{
			Notify: []NotificationTarget{
				{
					Type: NotificationTypeWebhook,
					SecretRef: &SecretKeyRef{
						Name: "webhook-secret",
						Key:  "url",
					},
					OnCompletion: true,
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error for valid SecretRef notification: %v", err)
	}
}

func TestTeamHealthRequestWebhook_InvalidNotificationBothURLAndSecretRef(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test-notify-both", Namespace: "default"},
		Spec: TeamHealthRequestSpec{
			Notify: []NotificationTarget{
				{
					Type: NotificationTypeWebhook,
					URL:  "https://example.com/webhook",
					SecretRef: &SecretKeyRef{
						Name: "webhook-secret",
						Key:  "url",
					},
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err == nil {
		t.Error("ValidateCreate() expected error when both URL and SecretRef are specified")
	}
}

func TestTeamHealthRequestWebhook_InvalidNotificationNoURLOrSecretRef(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test-notify-none", Namespace: "default"},
		Spec: TeamHealthRequestSpec{
			Notify: []NotificationTarget{
				{
					Type:         NotificationTypeWebhook,
					OnCompletion: true,
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err == nil {
		t.Error("ValidateCreate() expected error when neither URL nor SecretRef is specified")
	}
}

func TestTeamHealthRequestWebhook_InvalidNotificationHTTPWithoutAnnotation(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test-notify-http", Namespace: "default"},
		Spec: TeamHealthRequestSpec{
			Notify: []NotificationTarget{
				{
					Type: NotificationTypeWebhook,
					URL:  "http://example.com/webhook",
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err == nil {
		t.Error("ValidateCreate() expected error for HTTP URL without AllowHTTPWebhooksAnnotation")
	}
}

func TestTeamHealthRequestWebhook_ValidNotificationHTTPWithAnnotation(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-notify-http-ok",
			Namespace: "default",
			Annotations: map[string]string{
				AllowHTTPWebhooksAnnotation: "true",
			},
		},
		Spec: TeamHealthRequestSpec{
			Notify: []NotificationTarget{
				{
					Type: NotificationTypeWebhook,
					URL:  "http://example.com/webhook",
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error for HTTP URL with AllowHTTPWebhooksAnnotation: %v", err)
	}
}

func TestTeamHealthRequestWebhook_InvalidNotificationMalformedURL(t *testing.T) {
	v := &TeamHealthRequestCustomValidator{}
	hr := &TeamHealthRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test-notify-bad-url", Namespace: "default"},
		Spec: TeamHealthRequestSpec{
			Notify: []NotificationTarget{
				{
					Type: NotificationTypeWebhook,
					URL:  "://not-a-url",
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), hr)
	if err == nil {
		t.Error("ValidateCreate() expected error for malformed URL")
	}
}
