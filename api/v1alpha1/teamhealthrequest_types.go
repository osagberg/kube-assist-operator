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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CheckerName identifies a specific checker
// +kubebuilder:validation:Enum=workloads;helmreleases;kustomizations;gitrepositories;secrets;pvcs;quotas;networkpolicies
type CheckerName string

const (
	CheckerWorkloads       CheckerName = "workloads"
	CheckerHelmReleases    CheckerName = "helmreleases"
	CheckerKustomizations  CheckerName = "kustomizations"
	CheckerGitRepositories CheckerName = "gitrepositories"
	CheckerSecrets         CheckerName = "secrets"
	CheckerPVCs            CheckerName = "pvcs"
	CheckerQuotas          CheckerName = "quotas"
	CheckerNetworkPolicies CheckerName = "networkpolicies"
)

// ScopeConfig defines which namespaces to check
type ScopeConfig struct {
	// namespaces is an explicit list of namespaces to check
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// namespaceSelector selects namespaces by labels
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// currentNamespaceOnly limits checks to the namespace where the request is created
	// +optional
	// +kubebuilder:default=false
	CurrentNamespaceOnly bool `json:"currentNamespaceOnly,omitempty"`
}

// WorkloadCheckConfig contains configuration for workload checker
type WorkloadCheckConfig struct {
	// restartThreshold is the number of restarts that triggers a warning
	// +optional
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	RestartThreshold int32 `json:"restartThreshold,omitempty"`

	// includeJobs includes Job and CronJob resources
	// +optional
	// +kubebuilder:default=false
	IncludeJobs bool `json:"includeJobs,omitempty"`
}

// SecretCheckConfig contains configuration for secret checker
type SecretCheckConfig struct {
	// checkCertExpiry enables TLS certificate expiry checking
	// +optional
	// +kubebuilder:default=true
	CheckCertExpiry bool `json:"checkCertExpiry,omitempty"`

	// certExpiryWarningDays is the number of days before expiry to warn
	// +optional
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=1
	CertExpiryWarningDays int32 `json:"certExpiryWarningDays,omitempty"`

	// checkReferences verifies secrets are referenced by workloads
	// +optional
	// +kubebuilder:default=false
	CheckReferences bool `json:"checkReferences,omitempty"`
}

// QuotaCheckConfig contains configuration for quota checker
type QuotaCheckConfig struct {
	// usageWarningPercent is the percentage of quota usage that triggers a warning
	// +optional
	// +kubebuilder:default=80
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	UsageWarningPercent int32 `json:"usageWarningPercent,omitempty"`
}

// PVCCheckConfig contains configuration for PVC checker
type PVCCheckConfig struct {
	// capacityWarningPercent is the percentage of PVC capacity that triggers a warning
	// +optional
	// +kubebuilder:default=85
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	CapacityWarningPercent int32 `json:"capacityWarningPercent,omitempty"`
}

// CheckerConfig contains configuration for all checkers
type CheckerConfig struct {
	// workloads configuration
	// +optional
	Workloads *WorkloadCheckConfig `json:"workloads,omitempty"`

	// secrets configuration
	// +optional
	Secrets *SecretCheckConfig `json:"secrets,omitempty"`

	// quotas configuration
	// +optional
	Quotas *QuotaCheckConfig `json:"quotas,omitempty"`

	// pvcs configuration
	// +optional
	PVCs *PVCCheckConfig `json:"pvcs,omitempty"`
}

// NotificationType identifies a notification backend
// +kubebuilder:validation:Enum=webhook
type NotificationType string

const (
	NotificationTypeWebhook NotificationType = "webhook"

	// AllowHTTPWebhooksAnnotation permits HTTP webhook URLs for explicit test/dev use.
	AllowHTTPWebhooksAnnotation = "assist.cluster.local/allow-http-webhooks"
)

// SecretKeyRef is a reference to a key in a Secret
type SecretKeyRef struct {
	// name is the name of the Secret
	Name string `json:"name"`
	// key is the key within the Secret
	Key string `json:"key"`
}

// NotificationTarget defines where to send notifications
type NotificationTarget struct {
	// type is the notification backend type
	// +kubebuilder:validation:Required
	Type NotificationType `json:"type"`

	// url is the webhook URL (use this OR secretRef, not both)
	// +optional
	URL string `json:"url,omitempty"`

	// secretRef references a Secret containing the webhook URL
	// +optional
	SecretRef *SecretKeyRef `json:"secretRef,omitempty"`

	// onCompletion sends notification when health check completes
	// +optional
	// +kubebuilder:default=true
	OnCompletion bool `json:"onCompletion,omitempty"`

	// onSeverity sends notification only when issues of this severity or higher are found
	// +optional
	// +kubebuilder:validation:Enum=Critical;Warning;Info
	OnSeverity string `json:"onSeverity,omitempty"`
}

// TeamHealthRequestSpec defines the desired state of TeamHealthRequest
type TeamHealthRequestSpec struct {
	// scope defines which namespaces to check
	// +optional
	Scope ScopeConfig `json:"scope,omitempty"`

	// checks specifies which checkers to run (default: all available)
	// +optional
	Checks []CheckerName `json:"checks,omitempty"`

	// config contains checker-specific configuration
	// +optional
	Config CheckerConfig `json:"config,omitempty"`

	// ttlSecondsAfterFinished limits the lifetime of a completed/failed request.
	// After this duration, the request is automatically deleted.
	// If unset, the request is never auto-deleted.
	// +optional
	// +kubebuilder:validation:Minimum=0
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

	// notify configures notification targets for health check results
	// +optional
	Notify []NotificationTarget `json:"notify,omitempty"`
}

// CheckerResult contains results from a single checker
type CheckerResult struct {
	// healthy is the count of healthy resources
	Healthy int32 `json:"healthy"`

	// issues is the list of problems found
	// +optional
	Issues []DiagnosticIssue `json:"issues,omitempty"`

	// error is set if the checker failed to run
	// +optional
	Error string `json:"error,omitempty"`
}

// TeamHealthPhase represents the current phase of the request
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
type TeamHealthPhase string

const (
	TeamHealthPhasePending   TeamHealthPhase = "Pending"
	TeamHealthPhaseRunning   TeamHealthPhase = "Running"
	TeamHealthPhaseCompleted TeamHealthPhase = "Completed"
	TeamHealthPhaseFailed    TeamHealthPhase = "Failed"
)

// TeamHealthRequestStatus defines the observed state of TeamHealthRequest
type TeamHealthRequestStatus struct {
	// phase indicates the current phase of the health check
	// +optional
	Phase TeamHealthPhase `json:"phase,omitempty"`

	// summary provides a brief overview of findings
	// +optional
	Summary string `json:"summary,omitempty"`

	// results contains results grouped by checker
	// +optional
	Results map[string]CheckerResult `json:"results,omitempty"`

	// namespacesChecked lists the namespaces that were checked
	// +optional
	NamespacesChecked []string `json:"namespacesChecked,omitempty"`

	// lastCheckTime is when the health check was last performed
	// +optional
	LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`

	// completedAt is when the health check completed or failed
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// conditions represent the current state of the TeamHealthRequest
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Condition types for TeamHealthRequest
const (
	TeamHealthConditionNamespacesResolved = "NamespacesResolved"
	TeamHealthConditionCheckersCompleted  = "CheckersCompleted"
	TeamHealthConditionComplete           = "Complete"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Current phase"
// +kubebuilder:printcolumn:name="Summary",type="string",JSONPath=".status.summary",description="Brief summary"
// +kubebuilder:printcolumn:name="Namespaces",type="string",JSONPath=".status.namespacesChecked",description="Namespaces checked"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// TeamHealthRequest is the Schema for the teamhealthrequests API
type TeamHealthRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TeamHealthRequestSpec   `json:"spec,omitempty"`
	Status TeamHealthRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TeamHealthRequestList contains a list of TeamHealthRequest
type TeamHealthRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TeamHealthRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TeamHealthRequest{}, &TeamHealthRequestList{})
}
