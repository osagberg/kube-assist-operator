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

package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
)

// maxMutatingBodySize is the default body-size limit for mutating JSON endpoints.
const maxMutatingBodySize = 1 << 20 // 1 MB

// decodeJSONBody applies MaxBytesReader, strict JSON decoding, and trailing-token
// rejection. Returns true on success, false if an error response was already written.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) bool { //nolint:unparam // maxBytes varies by design; callers currently share the same default
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if isMaxBytesError(err) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		}
		return false
	}
	// Reject trailing tokens (e.g. `{"key":"v"}{"extra":"data"}`)
	if dec.More() {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return false
	}
	return true
}

// isMaxBytesError checks whether err (or any wrapped error) is a *http.MaxBytesError.
func isMaxBytesError(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

// Input limits for issue state keys.
const (
	maxIssueKeyLen    = 512
	maxIssueReasonLen = 1024
	maxSnoozeDuration = 24 * time.Hour
)

// issueKeyRe validates the expected format: namespace/resource/type.
var issueKeyRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?/.+/.+$`)

// validateIssueKey checks key format and length.
func validateIssueKey(key string) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if len(key) > maxIssueKeyLen {
		return fmt.Errorf("key exceeds maximum length of %d", maxIssueKeyLen)
	}
	if !issueKeyRe.MatchString(key) {
		return fmt.Errorf("key must match format namespace/resource/type")
	}
	return nil
}

// handleIssueAcknowledge handles POST and DELETE for /api/issues/acknowledge.
func (s *Server) handleIssueAcknowledge(w http.ResponseWriter, r *http.Request) {
	clusterID := r.URL.Query().Get("clusterId")

	switch r.Method {
	case http.MethodPost:
		var req struct {
			Key    string `json:"key"`
			Reason string `json:"reason,omitempty"`
		}
		if !decodeJSONBody(w, r, maxMutatingBodySize, &req) {
			return
		}
		if err := validateIssueKey(req.Key); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Reason) > maxIssueReasonLen {
			http.Error(w, fmt.Sprintf("reason exceeds maximum length of %d", maxIssueReasonLen), http.StatusBadRequest)
			return
		}
		state := &IssueState{
			Key:       req.Key,
			Action:    ActionAcknowledged,
			Reason:    req.Reason,
			CreatedAt: time.Now(),
		}
		s.mu.Lock()
		cs := s.getOrCreateClusterState(clusterID)
		cs.issueStates[req.Key] = state
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(state); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleIssueAcknowledge")
		}
	case http.MethodDelete:
		var req struct {
			Key string `json:"key"`
		}
		if !decodeJSONBody(w, r, maxMutatingBodySize, &req) {
			return
		}
		if err := validateIssueKey(req.Key); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		if cs, ok := s.clusters[clusterID]; ok {
			delete(cs.issueStates, req.Key)
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "removed"}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleIssueAcknowledge")
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleIssueSnooze handles POST and DELETE for /api/issues/snooze.
func (s *Server) handleIssueSnooze(w http.ResponseWriter, r *http.Request) {
	clusterID := r.URL.Query().Get("clusterId")

	switch r.Method {
	case http.MethodPost:
		var req struct {
			Key      string `json:"key"`
			Duration string `json:"duration"`
			Reason   string `json:"reason,omitempty"`
		}
		if !decodeJSONBody(w, r, maxMutatingBodySize, &req) {
			return
		}
		if err := validateIssueKey(req.Key); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Reason) > maxIssueReasonLen {
			http.Error(w, fmt.Sprintf("reason exceeds maximum length of %d", maxIssueReasonLen), http.StatusBadRequest)
			return
		}
		if req.Duration == "" {
			http.Error(w, "duration is required", http.StatusBadRequest)
			return
		}
		dur, err := time.ParseDuration(req.Duration)
		if err != nil {
			http.Error(w, "invalid duration: "+err.Error(), http.StatusBadRequest)
			return
		}
		if dur <= 0 {
			http.Error(w, "duration must be positive", http.StatusBadRequest)
			return
		}
		if dur > maxSnoozeDuration {
			http.Error(w, fmt.Sprintf("duration must be <= %s", maxSnoozeDuration), http.StatusBadRequest)
			return
		}
		snoozedUntil := time.Now().Add(dur)
		state := &IssueState{
			Key:          req.Key,
			Action:       ActionSnoozed,
			Reason:       req.Reason,
			SnoozedUntil: &snoozedUntil,
			CreatedAt:    time.Now(),
		}
		s.mu.Lock()
		cs := s.getOrCreateClusterState(clusterID)
		cs.issueStates[req.Key] = state
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(state); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleIssueSnooze")
		}
	case http.MethodDelete:
		var req struct {
			Key string `json:"key"`
		}
		if !decodeJSONBody(w, r, maxMutatingBodySize, &req) {
			return
		}
		if err := validateIssueKey(req.Key); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		if cs, ok := s.clusters[clusterID]; ok {
			delete(cs.issueStates, req.Key)
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "removed"}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleIssueSnooze")
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleIssueStates handles GET for /api/issue-states.
func (s *Server) handleIssueStates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	clusterID := r.URL.Query().Get("clusterId")

	s.mu.RLock()
	active := make(map[string]*IssueState)
	if cs, ok := s.clusters[clusterID]; ok {
		nowStates := time.Now()
		for k, st := range cs.issueStates {
			if st.Action == ActionSnoozed && st.SnoozedUntil != nil && st.SnoozedUntil.Before(nowStates) {
				continue
			}
			active[k] = st
		}
	}
	s.mu.RUnlock()

	if err := json.NewEncoder(w).Encode(active); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleIssueStates")
	}
}

// Troubleshoot request types and constants.

// CreateTroubleshootBody is the JSON body for POST /api/troubleshoot.
type CreateTroubleshootBody struct {
	Namespace string `json:"namespace"`
	Target    struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"target"`
	Actions   []string `json:"actions,omitempty"`
	TailLines int32    `json:"tailLines,omitempty"`
	TTL       *int32   `json:"ttlSecondsAfterFinished,omitempty"`
}

// CreateTroubleshootResponse is the JSON response for POST /api/troubleshoot.
type CreateTroubleshootResponse struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Phase     string `json:"phase"`
}

// TroubleshootRequestSummary is a brief summary of a TroubleshootRequest.
type TroubleshootRequestSummary struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Phase     string `json:"phase"`
	Target    struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"target"`
}

const (
	defaultTargetKind = "Deployment"
	defaultNamespace  = "default"
	maxTailLines      = int32(10000)
	maxTTLSeconds     = int32(86400) // 24 hours
)

// k8sNameRe matches valid Kubernetes resource names (RFC 1123 DNS label).
var k8sNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?$`)

var validTargetKinds = map[string]bool{
	defaultTargetKind: true,
	"StatefulSet":     true,
	"DaemonSet":       true,
	"Pod":             true,
	"ReplicaSet":      true,
}

var validActions = map[string]bool{
	"diagnose": true,
	"logs":     true,
	"events":   true,
	"describe": true,
	"all":      true,
}

// handleCreateTroubleshoot handles POST and GET for /api/troubleshoot.
func (s *Server) handleCreateTroubleshoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePostTroubleshoot(w, r)
	case http.MethodGet:
		s.handleListTroubleshoot(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePostTroubleshoot(w http.ResponseWriter, r *http.Request) {
	if s.k8sWriter == nil {
		http.Error(w, "TroubleshootRequest creation not available (console mode)", http.StatusServiceUnavailable)
		return
	}

	var body CreateTroubleshootBody
	if !decodeJSONBody(w, r, maxMutatingBodySize, &body) {
		return
	}

	// Validate target name (required, valid K8s name)
	if body.Target.Name == "" {
		http.Error(w, "target.name is required", http.StatusBadRequest)
		return
	}
	if !k8sNameRe.MatchString(body.Target.Name) {
		http.Error(w, "target.name must be a valid Kubernetes name (lowercase, alphanumeric, hyphens)", http.StatusBadRequest)
		return
	}

	// Default target kind
	if body.Target.Kind == "" {
		body.Target.Kind = defaultTargetKind
	}
	if !validTargetKinds[body.Target.Kind] {
		http.Error(w, "invalid target.kind: must be one of Deployment, StatefulSet, DaemonSet, Pod, ReplicaSet", http.StatusBadRequest)
		return
	}

	// Default namespace (validate if provided)
	if body.Namespace == "" {
		body.Namespace = defaultNamespace
	}
	if !k8sNameRe.MatchString(body.Namespace) {
		http.Error(w, "namespace must be a valid Kubernetes name (lowercase, alphanumeric, hyphens)", http.StatusBadRequest)
		return
	}

	// Default actions — normalize "all" (mutually exclusive with specifics)
	if len(body.Actions) == 0 {
		body.Actions = []string{"diagnose"}
	}
	for _, a := range body.Actions {
		if !validActions[a] {
			http.Error(w, fmt.Sprintf("invalid action %q: must be one of diagnose, logs, events, describe, all", a), http.StatusBadRequest)
			return
		}
	}
	// If "all" is specified, normalize to just ["all"]
	if slices.Contains(body.Actions, "all") {
		body.Actions = []string{"all"}
	}

	// Default and bound tailLines
	if body.TailLines <= 0 {
		body.TailLines = 100
	}
	if body.TailLines > maxTailLines {
		http.Error(w, fmt.Sprintf("tailLines must be <= %d", maxTailLines), http.StatusBadRequest)
		return
	}

	// Bound TTL
	if body.TTL != nil && (*body.TTL <= 0 || *body.TTL > maxTTLSeconds) {
		http.Error(w, fmt.Sprintf("ttlSecondsAfterFinished must be between 1 and %d", maxTTLSeconds), http.StatusBadRequest)
		return
	}

	actions := make([]assistv1alpha1.TroubleshootAction, len(body.Actions))
	for i, a := range body.Actions {
		actions[i] = assistv1alpha1.TroubleshootAction(a)
	}

	cr := &assistv1alpha1.TroubleshootRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dash-",
			Namespace:    body.Namespace,
		},
		Spec: assistv1alpha1.TroubleshootRequestSpec{
			Target: assistv1alpha1.TargetRef{
				Kind: body.Target.Kind,
				Name: body.Target.Name,
			},
			Actions:                 actions,
			TailLines:               body.TailLines,
			TTLSecondsAfterFinished: body.TTL,
		},
	}

	if err := s.k8sWriter.Create(r.Context(), cr); err != nil {
		log.Error(err, "Failed to create TroubleshootRequest")
		http.Error(w, "Failed to create TroubleshootRequest: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := CreateTroubleshootResponse{
		Name:      cr.Name,
		Namespace: cr.Namespace,
		Phase:     string(assistv1alpha1.PhasePending),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handlePostTroubleshoot")
	}
}

func (s *Server) handleListTroubleshoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.k8sWriter == nil {
		http.Error(w, "TroubleshootRequest listing not available (console mode)", http.StatusServiceUnavailable)
		return
	}

	var list assistv1alpha1.TroubleshootRequestList
	ns := r.URL.Query().Get("namespace")
	var opts []client.ListOption
	if ns != "" {
		opts = append(opts, client.InNamespace(ns))
	}

	if err := s.k8sWriter.List(r.Context(), &list, opts...); err != nil {
		log.Error(err, "Failed to list TroubleshootRequests")
		http.Error(w, "Failed to list TroubleshootRequests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	summaries := make([]TroubleshootRequestSummary, len(list.Items))
	for i, item := range list.Items {
		summaries[i] = TroubleshootRequestSummary{
			Name:      item.Name,
			Namespace: item.Namespace,
			Phase:     string(item.Status.Phase),
		}
		summaries[i].Target.Kind = item.Spec.Target.Kind
		summaries[i].Target.Name = item.Spec.Target.Name
	}

	if err := json.NewEncoder(w).Encode(summaries); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleListTroubleshoot")
	}
}
