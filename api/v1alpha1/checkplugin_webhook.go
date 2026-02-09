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
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupCheckPluginWebhookWithManager registers the webhook for CheckPlugin in the manager.
func SetupCheckPluginWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &CheckPlugin{}).
		WithValidator(&CheckPluginCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-assist-cluster-local-v1alpha1-checkplugin,mutating=false,failurePolicy=fail,sideEffects=None,groups=assist.cluster.local,resources=checkplugins,verbs=create;update,versions=v1alpha1,name=vcheckplugin.kb.io,admissionReviewVersions=v1

// CheckPluginCustomValidator validates CheckPlugin resources.
type CheckPluginCustomValidator struct{}

var _ admission.Validator[*CheckPlugin] = &CheckPluginCustomValidator{}

// ValidateCreate validates the CheckPlugin on creation.
func (v *CheckPluginCustomValidator) ValidateCreate(_ context.Context, cp *CheckPlugin) (admission.Warnings, error) {
	return v.validateCheckPlugin(cp)
}

// ValidateUpdate validates the CheckPlugin on update.
func (v *CheckPluginCustomValidator) ValidateUpdate(_ context.Context, _, cp *CheckPlugin) (admission.Warnings, error) {
	return v.validateCheckPlugin(cp)
}

// ValidateDelete validates the CheckPlugin on deletion.
func (v *CheckPluginCustomValidator) ValidateDelete(_ context.Context, _ *CheckPlugin) (admission.Warnings, error) {
	return nil, nil
}

// compileCEL validates that a CEL expression compiles successfully using the
// same environment as the plugin evaluator (object variable of dynamic type).
func compileCEL(env *cel.Env, expr string) error {
	_, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return issues.Err()
	}
	return nil
}

func (v *CheckPluginCustomValidator) validateCheckPlugin(cp *CheckPlugin) (admission.Warnings, error) {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	// rules must not be empty (also enforced by kubebuilder validation)
	if len(cp.Spec.Rules) == 0 {
		allErrs = append(allErrs, field.Required(specPath.Child("rules"), "at least one rule is required"))
	}

	// Create a CEL environment matching the plugin evaluator
	env, err := cel.NewEnv(cel.Variable("object", cel.DynType))
	if err != nil {
		allErrs = append(allErrs, field.InternalError(specPath, fmt.Errorf("failed to create CEL environment: %w", err)))
		return nil, allErrs.ToAggregate()
	}

	// Validate each rule's CEL expressions
	for i, rule := range cp.Spec.Rules {
		rulePath := specPath.Child("rules").Index(i)

		// Validate condition CEL expression
		if rule.Condition == "" {
			allErrs = append(allErrs, field.Required(rulePath.Child("condition"), "condition is required"))
		} else if err := compileCEL(env, rule.Condition); err != nil {
			allErrs = append(allErrs, field.Invalid(rulePath.Child("condition"), rule.Condition,
				fmt.Sprintf("invalid CEL expression: %v", err)))
		}

		// Validate message — if it starts with "=", it's a CEL expression
		if strings.HasPrefix(rule.Message, "=") {
			celExpr := rule.Message[1:]
			if err := compileCEL(env, celExpr); err != nil {
				allErrs = append(allErrs, field.Invalid(rulePath.Child("message"), rule.Message,
					fmt.Sprintf("invalid CEL message expression: %v", err)))
			}
		}
	}

	if len(allErrs) > 0 {
		return nil, allErrs.ToAggregate()
	}
	return nil, nil
}
