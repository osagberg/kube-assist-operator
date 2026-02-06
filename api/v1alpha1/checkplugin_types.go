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

// CheckPluginSpec defines a custom health check using CEL expressions
type CheckPluginSpec struct {
	// displayName is the human-readable name for this plugin
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	DisplayName string `json:"displayName"`

	// description explains what this plugin checks
	// +optional
	Description string `json:"description,omitempty"`

	// targetResource identifies the GVR of resources to check
	// +kubebuilder:validation:Required
	TargetResource TargetGVR `json:"targetResource"`

	// rules defines the CEL-based health check rules
	// +kubebuilder:validation:MinItems=1
	Rules []CheckRule `json:"rules"`
}

// TargetGVR identifies a Kubernetes resource type by Group/Version/Kind
type TargetGVR struct {
	// group is the API group (empty string for core resources)
	// +optional
	Group string `json:"group,omitempty"`

	// version is the API version
	// +kubebuilder:validation:Required
	Version string `json:"version"`

	// resource is the plural resource name (e.g., "deployments", "pods")
	// +kubebuilder:validation:Required
	Resource string `json:"resource"`

	// kind is the resource kind (e.g., "Deployment", "Pod")
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`
}

// CheckRule defines a single CEL-based health check rule
type CheckRule struct {
	// name identifies this rule within the plugin
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// condition is a CEL expression that evaluates to true when an issue is detected.
	// The expression has access to the object via `object` variable (unstructured).
	// Example: `object.status.readyReplicas < object.spec.replicas`
	// +kubebuilder:validation:Required
	Condition string `json:"condition"`

	// severity is the issue severity when this rule fires
	// +kubebuilder:validation:Enum=Critical;Warning;Info
	// +kubebuilder:default=Warning
	Severity string `json:"severity,omitempty"`

	// message is a CEL expression or static string for the issue message.
	// Can use `object` variable. If it starts with `=`, it's treated as CEL.
	// Example: `="Deployment " + object.metadata.name + " has unavailable replicas"`
	// +kubebuilder:validation:Required
	Message string `json:"message"`
}

// CheckPluginStatus defines the observed state of CheckPlugin
type CheckPluginStatus struct {
	// ready indicates if the plugin is compiled and ready
	// +optional
	Ready bool `json:"ready,omitempty"`

	// error contains the last compilation error if any
	// +optional
	Error string `json:"error,omitempty"`

	// lastUpdated is when the plugin was last processed
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Name",type="string",JSONPath=".spec.displayName",description="Display name"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready",description="Plugin ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CheckPlugin is the Schema for the checkplugins API
type CheckPlugin struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CheckPluginSpec   `json:"spec,omitempty"`
	Status CheckPluginStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CheckPluginList contains a list of CheckPlugin
type CheckPluginList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CheckPlugin `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CheckPlugin{}, &CheckPluginList{})
}
