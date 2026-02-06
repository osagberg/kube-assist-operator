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

	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupTroubleshootRequestWebhookWithManager registers the webhook for TroubleshootRequest in the manager.
func SetupTroubleshootRequestWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &TroubleshootRequest{}).
		WithValidator(&TroubleshootRequestCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-assist-cluster-local-v1alpha1-troubleshootrequest,mutating=false,failurePolicy=fail,sideEffects=None,groups=assist.cluster.local,resources=troubleshootrequests,verbs=create;update,versions=v1alpha1,name=vtroubleshootrequest.kb.io,admissionReviewVersions=v1

// TroubleshootRequestCustomValidator validates TroubleshootRequest resources.
type TroubleshootRequestCustomValidator struct{}

var _ admission.Validator[*TroubleshootRequest] = &TroubleshootRequestCustomValidator{}

// validTargetKinds are the allowed values for spec.target.kind.
var validTargetKinds = map[string]bool{
	"Deployment":  true,
	"StatefulSet": true,
	"DaemonSet":   true,
	"Pod":         true,
	"ReplicaSet":  true,
}

// validActions are the allowed values for spec.actions.
var validActions = map[TroubleshootAction]bool{
	ActionDiagnose: true,
	ActionLogs:     true,
	ActionEvents:   true,
	ActionDescribe: true,
	ActionAll:      true,
}

// ValidateCreate validates the TroubleshootRequest on creation.
func (v *TroubleshootRequestCustomValidator) ValidateCreate(_ context.Context, tr *TroubleshootRequest) (admission.Warnings, error) {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	// target.name must be non-empty
	if tr.Spec.Target.Name == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("target", "name"), "target name is required"))
	}

	// target.kind must be valid (if set)
	if tr.Spec.Target.Kind != "" && !validTargetKinds[tr.Spec.Target.Kind] {
		allErrs = append(allErrs, field.NotSupported(specPath.Child("target", "kind"),
			tr.Spec.Target.Kind, validTargetKindsList()))
	}

	// actions must be valid
	for i, action := range tr.Spec.Actions {
		if !validActions[action] {
			allErrs = append(allErrs, field.NotSupported(specPath.Child("actions").Index(i),
				string(action), validActionsList()))
		}
	}

	// ttlSecondsAfterFinished must be >= 0
	if tr.Spec.TTLSecondsAfterFinished != nil && *tr.Spec.TTLSecondsAfterFinished < 0 {
		allErrs = append(allErrs, field.Invalid(specPath.Child("ttlSecondsAfterFinished"),
			*tr.Spec.TTLSecondsAfterFinished, "must be >= 0"))
	}

	if len(allErrs) > 0 {
		return nil, allErrs.ToAggregate()
	}
	return nil, nil
}

// ValidateUpdate validates the TroubleshootRequest on update.
// Target fields (name, kind) are immutable after creation.
func (v *TroubleshootRequestCustomValidator) ValidateUpdate(_ context.Context, oldTR, newTR *TroubleshootRequest) (admission.Warnings, error) {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	// Check immutability of spec fields
	if newTR.Spec.Target.Name != oldTR.Spec.Target.Name {
		allErrs = append(allErrs, field.Forbidden(specPath.Child("target", "name"), "field is immutable"))
	}
	if newTR.Spec.Target.Kind != oldTR.Spec.Target.Kind {
		allErrs = append(allErrs, field.Forbidden(specPath.Child("target", "kind"), "field is immutable"))
	}

	if len(allErrs) > 0 {
		return nil, allErrs.ToAggregate()
	}
	return nil, nil
}

// ValidateDelete validates the TroubleshootRequest on deletion.
func (v *TroubleshootRequestCustomValidator) ValidateDelete(_ context.Context, _ *TroubleshootRequest) (admission.Warnings, error) {
	return nil, nil
}

func validTargetKindsList() []string {
	kinds := make([]string, 0, len(validTargetKinds))
	for k := range validTargetKinds {
		kinds = append(kinds, k)
	}
	return kinds
}

func validActionsList() []string {
	actions := make([]string, 0, len(validActions))
	for a := range validActions {
		actions = append(actions, string(a))
	}
	return actions
}
