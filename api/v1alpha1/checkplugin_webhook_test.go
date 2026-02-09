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
)

func TestCheckPluginWebhook_ValidCreate(t *testing.T) {
	v := &CheckPluginCustomValidator{}
	cp := &CheckPlugin{
		Spec: CheckPluginSpec{
			DisplayName: "test-plugin",
			TargetResource: TargetGVR{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
				Kind:     "Deployment",
			},
			Rules: []CheckRule{
				{
					Name:      "check-replicas",
					Condition: "object.spec.replicas < 2",
					Severity:  "Warning",
					Message:   "Deployment has less than 2 replicas",
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), cp)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error: %v", err)
	}
}

func TestCheckPluginWebhook_InvalidCELCondition(t *testing.T) {
	v := &CheckPluginCustomValidator{}
	cp := &CheckPlugin{
		Spec: CheckPluginSpec{
			DisplayName: "bad-plugin",
			TargetResource: TargetGVR{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
				Kind:     "Deployment",
			},
			Rules: []CheckRule{
				{
					Name:      "bad-rule",
					Condition: "this is not valid CEL !!!",
					Severity:  "Warning",
					Message:   "Some message",
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), cp)
	if err == nil {
		t.Error("ValidateCreate() expected error for invalid CEL condition")
	}
}

func TestCheckPluginWebhook_InvalidCELMessage(t *testing.T) {
	v := &CheckPluginCustomValidator{}
	cp := &CheckPlugin{
		Spec: CheckPluginSpec{
			DisplayName: "bad-msg-plugin",
			TargetResource: TargetGVR{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
				Kind:     "Deployment",
			},
			Rules: []CheckRule{
				{
					Name:      "bad-msg-rule",
					Condition: "true",
					Severity:  "Warning",
					Message:   "=this is not valid CEL either !!!",
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), cp)
	if err == nil {
		t.Error("ValidateCreate() expected error for invalid CEL message expression")
	}
}

func TestCheckPluginWebhook_ValidCELMessage(t *testing.T) {
	v := &CheckPluginCustomValidator{}
	cp := &CheckPlugin{
		Spec: CheckPluginSpec{
			DisplayName: "cel-msg-plugin",
			TargetResource: TargetGVR{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
				Kind:     "Deployment",
			},
			Rules: []CheckRule{
				{
					Name:      "cel-msg-rule",
					Condition: "true",
					Severity:  "Warning",
					Message:   "='Deployment ' + object.metadata.name + ' has issues'",
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), cp)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error: %v", err)
	}
}

func TestCheckPluginWebhook_StaticMessage(t *testing.T) {
	v := &CheckPluginCustomValidator{}
	cp := &CheckPlugin{
		Spec: CheckPluginSpec{
			DisplayName: "static-msg-plugin",
			TargetResource: TargetGVR{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
				Kind:     "Deployment",
			},
			Rules: []CheckRule{
				{
					Name:      "static-rule",
					Condition: "true",
					Severity:  "Info",
					Message:   "This is a static message",
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), cp)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error: %v", err)
	}
}

func TestCheckPluginWebhook_EmptyRules(t *testing.T) {
	v := &CheckPluginCustomValidator{}
	cp := &CheckPlugin{
		Spec: CheckPluginSpec{
			DisplayName: "no-rules",
			TargetResource: TargetGVR{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
				Kind:     "Deployment",
			},
			Rules: []CheckRule{},
		},
	}
	_, err := v.ValidateCreate(context.Background(), cp)
	if err == nil {
		t.Error("ValidateCreate() expected error for empty rules")
	}
}

func TestCheckPluginWebhook_MultipleRulesOneInvalid(t *testing.T) {
	v := &CheckPluginCustomValidator{}
	cp := &CheckPlugin{
		Spec: CheckPluginSpec{
			DisplayName: "mixed-rules",
			TargetResource: TargetGVR{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
				Kind:     "Deployment",
			},
			Rules: []CheckRule{
				{
					Name:      "good-rule",
					Condition: "object.spec.replicas < 2",
					Severity:  "Warning",
					Message:   "Low replicas",
				},
				{
					Name:      "bad-rule",
					Condition: "invalid CEL here !!!",
					Severity:  "Warning",
					Message:   "Never reached",
				},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), cp)
	if err == nil {
		t.Error("ValidateCreate() expected error when one rule has invalid CEL")
	}
}

func TestCheckPluginWebhook_UpdateValidation(t *testing.T) {
	v := &CheckPluginCustomValidator{}
	old := &CheckPlugin{
		Spec: CheckPluginSpec{
			DisplayName: "plugin",
			TargetResource: TargetGVR{
				Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment",
			},
			Rules: []CheckRule{
				{Name: "r1", Condition: "true", Message: "ok"},
			},
		},
	}
	updated := &CheckPlugin{
		Spec: CheckPluginSpec{
			DisplayName: "plugin",
			TargetResource: TargetGVR{
				Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment",
			},
			Rules: []CheckRule{
				{Name: "r1", Condition: "invalid !!!", Message: "ok"},
			},
		},
	}
	_, err := v.ValidateUpdate(context.Background(), old, updated)
	if err == nil {
		t.Error("ValidateUpdate() expected error for invalid CEL on update")
	}
}

func TestCheckPluginWebhook_DeleteAlwaysAllowed(t *testing.T) {
	v := &CheckPluginCustomValidator{}
	_, err := v.ValidateDelete(context.Background(), &CheckPlugin{})
	if err != nil {
		t.Errorf("ValidateDelete() unexpected error: %v", err)
	}
}
