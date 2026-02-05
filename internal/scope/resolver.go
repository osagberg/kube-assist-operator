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

// Package scope provides namespace resolution for health checks.
package scope

import (
	"context"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
)

// Resolver resolves namespace scopes to actual namespace names
type Resolver struct {
	client           client.Client
	defaultNamespace string
}

// NewResolver creates a new namespace resolver
func NewResolver(cl client.Client, defaultNamespace string) *Resolver {
	return &Resolver{
		client:           cl,
		defaultNamespace: defaultNamespace,
	}
}

// ResolveNamespaces resolves a ScopeConfig to a list of namespace names
// Priority: explicit list > selector > current namespace
func (r *Resolver) ResolveNamespaces(ctx context.Context, scope assistv1alpha1.ScopeConfig) ([]string, error) {
	// Priority 1: Explicit namespace list
	if len(scope.Namespaces) > 0 {
		return r.validateNamespaces(ctx, scope.Namespaces)
	}

	// Priority 2: Label selector
	if scope.NamespaceSelector != nil {
		return r.resolveBySelector(ctx, scope.NamespaceSelector)
	}

	// Priority 3: Current namespace only (or if no scope specified)
	if scope.CurrentNamespaceOnly || r.isEmptyScope(scope) {
		return []string{r.defaultNamespace}, nil
	}

	// Fallback to current namespace
	return []string{r.defaultNamespace}, nil
}

// validateNamespaces checks that all specified namespaces exist
func (r *Resolver) validateNamespaces(ctx context.Context, namespaces []string) ([]string, error) {
	valid := make([]string, 0, len(namespaces))

	for _, ns := range namespaces {
		var namespace corev1.Namespace
		if err := r.client.Get(ctx, client.ObjectKey{Name: ns}, &namespace); err != nil {
			// Skip non-existent namespaces but don't fail
			continue
		}
		valid = append(valid, ns)
	}

	// Sort for consistent ordering
	sort.Strings(valid)
	return valid, nil
}

// resolveBySelector returns namespaces matching the label selector
func (r *Resolver) resolveBySelector(ctx context.Context, selector *metav1.LabelSelector) ([]string, error) {
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}

	var nsList corev1.NamespaceList
	if err := r.client.List(ctx, &nsList, client.MatchingLabelsSelector{Selector: sel}); err != nil {
		return nil, err
	}

	namespaces := make([]string, 0, len(nsList.Items))
	for _, ns := range nsList.Items {
		// Skip terminating namespaces
		if ns.Status.Phase == corev1.NamespaceTerminating {
			continue
		}
		namespaces = append(namespaces, ns.Name)
	}

	// Sort for consistent ordering
	sort.Strings(namespaces)
	return namespaces, nil
}

// isEmptyScope returns true if no scope is specified
func (r *Resolver) isEmptyScope(scope assistv1alpha1.ScopeConfig) bool {
	return len(scope.Namespaces) == 0 &&
		scope.NamespaceSelector == nil &&
		!scope.CurrentNamespaceOnly
}

// FilterSystemNamespaces removes common system namespaces from the list
func FilterSystemNamespaces(namespaces []string) []string {
	systemNamespaces := map[string]bool{
		"kube-system":     true,
		"kube-public":     true,
		"kube-node-lease": true,
		"flux-system":     true,
	}

	filtered := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		if !systemNamespaces[ns] {
			filtered = append(filtered, ns)
		}
	}
	return filtered
}
