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
	"net/url"

	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupTeamHealthRequestWebhookWithManager registers the webhook for TeamHealthRequest in the manager.
func SetupTeamHealthRequestWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &TeamHealthRequest{}).
		WithValidator(&TeamHealthRequestCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-assist-cluster-local-v1alpha1-teamhealthrequest,mutating=false,failurePolicy=fail,sideEffects=None,groups=assist.cluster.local,resources=teamhealthrequests,verbs=create;update,versions=v1alpha1,name=vteamhealthrequest.kb.io,admissionReviewVersions=v1

// TeamHealthRequestCustomValidator validates TeamHealthRequest resources.
type TeamHealthRequestCustomValidator struct{}

var _ admission.Validator[*TeamHealthRequest] = &TeamHealthRequestCustomValidator{}

// validCheckerNames are the allowed values for spec.checks[].
var validCheckerNames = map[CheckerName]bool{
	CheckerWorkloads:       true,
	CheckerHelmReleases:    true,
	CheckerKustomizations:  true,
	CheckerGitRepositories: true,
	CheckerSecrets:         true,
	CheckerPVCs:            true,
	CheckerQuotas:          true,
	CheckerNetworkPolicies: true,
}

// ValidateCreate validates the TeamHealthRequest on creation.
func (v *TeamHealthRequestCustomValidator) ValidateCreate(_ context.Context, hr *TeamHealthRequest) (admission.Warnings, error) {
	allErrs := v.validateSpec(hr)
	if len(allErrs) > 0 {
		return nil, allErrs.ToAggregate()
	}
	return nil, nil
}

// ValidateUpdate validates the TeamHealthRequest on update.
// Scope's currentNamespaceOnly is immutable after creation.
func (v *TeamHealthRequestCustomValidator) ValidateUpdate(_ context.Context, oldHR, newHR *TeamHealthRequest) (admission.Warnings, error) {
	allErrs := v.validateSpec(newHR)

	specPath := field.NewPath("spec")

	// Check scope immutability
	if newHR.Spec.Scope.CurrentNamespaceOnly != oldHR.Spec.Scope.CurrentNamespaceOnly {
		allErrs = append(allErrs, field.Forbidden(specPath.Child("scope", "currentNamespaceOnly"), "field is immutable"))
	}

	if len(allErrs) > 0 {
		return nil, allErrs.ToAggregate()
	}
	return nil, nil
}

// validateSpec performs common validation for TeamHealthRequest spec.
func (v *TeamHealthRequestCustomValidator) validateSpec(hr *TeamHealthRequest) field.ErrorList {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	// checks must contain valid checker names
	for i, check := range hr.Spec.Checks {
		if !validCheckerNames[check] {
			allErrs = append(allErrs, field.NotSupported(specPath.Child("checks").Index(i),
				string(check), validCheckerNamesList()))
		}
	}

	// ttlSecondsAfterFinished must be >= 0
	if hr.Spec.TTLSecondsAfterFinished != nil && *hr.Spec.TTLSecondsAfterFinished < 0 {
		allErrs = append(allErrs, field.Invalid(specPath.Child("ttlSecondsAfterFinished"),
			*hr.Spec.TTLSecondsAfterFinished, "must be >= 0"))
	}

	// Validate notification targets
	for i, target := range hr.Spec.Notify {
		notifyPath := specPath.Child("notify").Index(i)
		if target.URL == "" && target.SecretRef == nil {
			allErrs = append(allErrs, field.Required(notifyPath, "must specify url or secretRef"))
		}
		if target.URL != "" && target.SecretRef != nil {
			allErrs = append(allErrs, field.Forbidden(notifyPath, "cannot specify both url and secretRef"))
		}
		if target.URL != "" {
			u, err := url.Parse(target.URL)
			if err != nil {
				allErrs = append(allErrs, field.Invalid(notifyPath.Child("url"), target.URL, "invalid URL"))
			} else if u.Scheme != "https" {
				// Allow HTTP only if annotation is set
				if hr.Annotations == nil || hr.Annotations[AllowHTTPWebhooksAnnotation] != "true" {
					allErrs = append(allErrs, field.Invalid(notifyPath.Child("url"), target.URL,
						"webhook URL must use HTTPS (annotate with "+AllowHTTPWebhooksAnnotation+"=true to override)"))
				}
			}
		}
	}

	// scope: can't set both currentNamespaceOnly and namespaces/namespaceSelector
	if hr.Spec.Scope.CurrentNamespaceOnly {
		if len(hr.Spec.Scope.Namespaces) > 0 {
			allErrs = append(allErrs, field.Forbidden(specPath.Child("scope", "namespaces"),
				"cannot set namespaces when currentNamespaceOnly is true"))
		}
		if hr.Spec.Scope.NamespaceSelector != nil {
			allErrs = append(allErrs, field.Forbidden(specPath.Child("scope", "namespaceSelector"),
				"cannot set namespaceSelector when currentNamespaceOnly is true"))
		}
	}

	// Detect duplicate namespaces
	if len(hr.Spec.Scope.Namespaces) > 0 {
		seen := make(map[string]bool, len(hr.Spec.Scope.Namespaces))
		for i, ns := range hr.Spec.Scope.Namespaces {
			if seen[ns] {
				allErrs = append(allErrs, field.Duplicate(specPath.Child("scope", "namespaces").Index(i), ns))
			}
			seen[ns] = true
		}
	}

	return allErrs
}

// ValidateDelete validates the TeamHealthRequest on deletion.
func (v *TeamHealthRequestCustomValidator) ValidateDelete(_ context.Context, _ *TeamHealthRequest) (admission.Warnings, error) {
	return nil, nil
}

func validCheckerNamesList() []string {
	names := make([]string, 0, len(validCheckerNames))
	for n := range validCheckerNames {
		names = append(names, string(n))
	}
	return names
}
