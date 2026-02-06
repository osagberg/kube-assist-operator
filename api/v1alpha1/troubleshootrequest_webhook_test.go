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

func TestTroubleshootRequestWebhook_ValidCreate(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	tr := &TroubleshootRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: TroubleshootRequestSpec{
			Target:  TargetRef{Kind: "Deployment", Name: "my-app"},
			Actions: []TroubleshootAction{ActionDiagnose},
		},
	}
	_, err := v.ValidateCreate(context.Background(), tr)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error: %v", err)
	}
}

func TestTroubleshootRequestWebhook_EmptyTargetName(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	tr := &TroubleshootRequest{
		Spec: TroubleshootRequestSpec{
			Target:  TargetRef{Kind: "Deployment", Name: ""},
			Actions: []TroubleshootAction{ActionDiagnose},
		},
	}
	_, err := v.ValidateCreate(context.Background(), tr)
	if err == nil {
		t.Error("ValidateCreate() expected error for empty target name")
	}
}

func TestTroubleshootRequestWebhook_InvalidTargetKind(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	tr := &TroubleshootRequest{
		Spec: TroubleshootRequestSpec{
			Target:  TargetRef{Kind: "CronJob", Name: "my-app"},
			Actions: []TroubleshootAction{ActionDiagnose},
		},
	}
	_, err := v.ValidateCreate(context.Background(), tr)
	if err == nil {
		t.Error("ValidateCreate() expected error for invalid target kind")
	}
}

func TestTroubleshootRequestWebhook_InvalidAction(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	tr := &TroubleshootRequest{
		Spec: TroubleshootRequestSpec{
			Target:  TargetRef{Kind: "Deployment", Name: "my-app"},
			Actions: []TroubleshootAction{"invalid-action"},
		},
	}
	_, err := v.ValidateCreate(context.Background(), tr)
	if err == nil {
		t.Error("ValidateCreate() expected error for invalid action")
	}
}

func TestTroubleshootRequestWebhook_NegativeTTL(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	tr := &TroubleshootRequest{
		Spec: TroubleshootRequestSpec{
			Target:                  TargetRef{Kind: "Deployment", Name: "my-app"},
			TTLSecondsAfterFinished: ptr.To(int32(-1)),
		},
	}
	_, err := v.ValidateCreate(context.Background(), tr)
	if err == nil {
		t.Error("ValidateCreate() expected error for negative TTL")
	}
}

func TestTroubleshootRequestWebhook_ValidTTL(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	tr := &TroubleshootRequest{
		Spec: TroubleshootRequestSpec{
			Target:                  TargetRef{Kind: "Deployment", Name: "my-app"},
			TTLSecondsAfterFinished: ptr.To(int32(60)),
		},
	}
	_, err := v.ValidateCreate(context.Background(), tr)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error: %v", err)
	}
}

func TestTroubleshootRequestWebhook_ZeroTTL(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	tr := &TroubleshootRequest{
		Spec: TroubleshootRequestSpec{
			Target:                  TargetRef{Kind: "Deployment", Name: "my-app"},
			TTLSecondsAfterFinished: ptr.To(int32(0)),
		},
	}
	_, err := v.ValidateCreate(context.Background(), tr)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error for TTL=0: %v", err)
	}
}

func TestTroubleshootRequestWebhook_ImmutableSpec(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	old := &TroubleshootRequest{
		Spec: TroubleshootRequestSpec{
			Target: TargetRef{Kind: "Deployment", Name: "my-app"},
		},
	}
	updated := &TroubleshootRequest{
		Spec: TroubleshootRequestSpec{
			Target: TargetRef{Kind: "StatefulSet", Name: "other-app"},
		},
	}
	_, err := v.ValidateUpdate(context.Background(), old, updated)
	if err == nil {
		t.Error("ValidateUpdate() expected error for spec change")
	}
}

func TestTroubleshootRequestWebhook_AllowStatusUpdate(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	old := &TroubleshootRequest{
		Spec: TroubleshootRequestSpec{
			Target: TargetRef{Kind: "Deployment", Name: "my-app"},
		},
	}
	updated := &TroubleshootRequest{
		Spec: TroubleshootRequestSpec{
			Target: TargetRef{Kind: "Deployment", Name: "my-app"},
		},
		Status: TroubleshootRequestStatus{
			Phase: PhaseCompleted,
		},
	}
	_, err := v.ValidateUpdate(context.Background(), old, updated)
	if err != nil {
		t.Errorf("ValidateUpdate() unexpected error: %v", err)
	}
}

func TestTroubleshootRequestWebhook_DeleteAlwaysAllowed(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	_, err := v.ValidateDelete(context.Background(), &TroubleshootRequest{})
	if err != nil {
		t.Errorf("ValidateDelete() unexpected error: %v", err)
	}
}

func TestTroubleshootRequestWebhook_AllValidKinds(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	for _, kind := range []string{"Deployment", "StatefulSet", "DaemonSet", "Pod", "ReplicaSet"} {
		tr := &TroubleshootRequest{
			Spec: TroubleshootRequestSpec{
				Target: TargetRef{Kind: kind, Name: "my-app"},
			},
		}
		_, err := v.ValidateCreate(context.Background(), tr)
		if err != nil {
			t.Errorf("ValidateCreate() unexpected error for kind %s: %v", kind, err)
		}
	}
}

func TestTroubleshootRequestWebhook_AllValidActions(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	for _, action := range []TroubleshootAction{ActionDiagnose, ActionLogs, ActionEvents, ActionDescribe, ActionAll} {
		tr := &TroubleshootRequest{
			Spec: TroubleshootRequestSpec{
				Target:  TargetRef{Kind: "Deployment", Name: "my-app"},
				Actions: []TroubleshootAction{action},
			},
		}
		_, err := v.ValidateCreate(context.Background(), tr)
		if err != nil {
			t.Errorf("ValidateCreate() unexpected error for action %s: %v", action, err)
		}
	}
}

func TestTroubleshootRequestWebhook_EmptyKindAllowed(t *testing.T) {
	v := &TroubleshootRequestCustomValidator{}
	tr := &TroubleshootRequest{
		Spec: TroubleshootRequestSpec{
			Target: TargetRef{Kind: "", Name: "my-app"},
		},
	}
	_, err := v.ValidateCreate(context.Background(), tr)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error for empty kind (should default): %v", err)
	}
}
