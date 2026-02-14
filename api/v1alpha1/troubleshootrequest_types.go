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

// TroubleshootAction defines what diagnostic action to perform
// +kubebuilder:validation:Enum=diagnose;logs;events;describe;all
type TroubleshootAction string

const (
	// ActionDiagnose analyzes why a workload might be failing
	ActionDiagnose TroubleshootAction = "diagnose"
	// ActionLogs collects logs from the target workload
	ActionLogs TroubleshootAction = "logs"
	// ActionEvents collects recent events for the target
	ActionEvents TroubleshootAction = "events"
	// ActionDescribe provides detailed resource information
	ActionDescribe TroubleshootAction = "describe"
	// ActionAll performs all diagnostic actions
	ActionAll TroubleshootAction = "all"
)

// TargetRef identifies the workload to troubleshoot
type TargetRef struct {
	// kind is the type of resource (Deployment, StatefulSet, Pod, etc.)
	// +kubebuilder:validation:Enum=Deployment;StatefulSet;DaemonSet;Pod;ReplicaSet
	// +kubebuilder:default=Deployment
	Kind string `json:"kind,omitempty"`

	// name is the name of the resource to troubleshoot
	// +required
	Name string `json:"name"`
}

// TroubleshootRequestSpec defines the desired state of TroubleshootRequest
type TroubleshootRequestSpec struct {
	// target identifies the workload to troubleshoot
	// +required
	Target TargetRef `json:"target"`

	// actions specifies what diagnostic actions to perform
	// +optional
	// +kubebuilder:default={"diagnose"}
	Actions []TroubleshootAction `json:"actions,omitempty"`

	// tailLines specifies how many log lines to collect (default 100)
	// +optional
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10000
	TailLines int32 `json:"tailLines,omitempty"`

	// ttlSecondsAfterFinished limits the lifetime of a completed/failed request.
	// After this duration, the request is automatically deleted.
	// If unset, the request is never auto-deleted.
	// +optional
	// +kubebuilder:validation:Minimum=0
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
}

// TroubleshootPhase represents the current phase of the request
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
type TroubleshootPhase string

const (
	PhasePending   TroubleshootPhase = "Pending"
	PhaseRunning   TroubleshootPhase = "Running"
	PhaseCompleted TroubleshootPhase = "Completed"
	PhaseFailed    TroubleshootPhase = "Failed"
)

// DiagnosticIssue represents a discovered problem
type DiagnosticIssue struct {
	// type categorizes the issue
	Type string `json:"type"`

	// severity indicates how serious the issue is
	// +kubebuilder:validation:Enum=Critical;Warning;Info
	Severity string `json:"severity"`

	// message is a human-readable description of the issue
	Message string `json:"message"`

	// suggestion provides actionable advice to fix the issue
	// +optional
	Suggestion string `json:"suggestion,omitempty"`
}

// TroubleshootRequestStatus defines the observed state of TroubleshootRequest
type TroubleshootRequestStatus struct {
	// observedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// phase indicates the current phase of the troubleshooting request
	// +optional
	Phase TroubleshootPhase `json:"phase,omitempty"`

	// summary provides a brief overview of findings
	// +optional
	Summary string `json:"summary,omitempty"`

	// issues lists all discovered problems
	// +optional
	Issues []DiagnosticIssue `json:"issues,omitempty"`

	// logsConfigMap references the ConfigMap containing collected logs
	// +optional
	LogsConfigMap string `json:"logsConfigMap,omitempty"`

	// eventsConfigMap references the ConfigMap containing collected events
	// +optional
	EventsConfigMap string `json:"eventsConfigMap,omitempty"`

	// startedAt is when the troubleshooting started
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// completedAt is when the troubleshooting completed
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// conditions represent the current state of the TroubleshootRequest
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Condition types for TroubleshootRequest
const (
	ConditionTargetFound     = "TargetFound"
	ConditionDiagnosed       = "Diagnosed"
	ConditionLogsCollected   = "LogsCollected"
	ConditionEventsCollected = "EventsCollected"
	ConditionComplete        = "Complete"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target",type="string",JSONPath=".spec.target.name",description="Target workload"
// +kubebuilder:printcolumn:name="Kind",type="string",JSONPath=".spec.target.kind",description="Target kind"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Current phase"
// +kubebuilder:printcolumn:name="Summary",type="string",JSONPath=".status.summary",description="Brief summary",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// TroubleshootRequest is the Schema for the troubleshootrequests API
type TroubleshootRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec   TroubleshootRequestSpec   `json:"spec"`
	Status TroubleshootRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TroubleshootRequestList contains a list of TroubleshootRequest
type TroubleshootRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TroubleshootRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TroubleshootRequest{}, &TroubleshootRequestList{})
}
