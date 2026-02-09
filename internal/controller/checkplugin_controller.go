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

package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/checker/plugin"
)

const checkPluginFinalizer = "assist.cluster.local/checkplugin-finalizer"

var checkPluginLog = logf.Log.WithName("checkplugin")

// CheckPluginReconciler reconciles a CheckPlugin object
type CheckPluginReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry *checker.Registry
}

// +kubebuilder:rbac:groups=assist.cluster.local,resources=checkplugins,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=assist.cluster.local,resources=checkplugins/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=assist.cluster.local,resources=checkplugins/finalizers,verbs=update
// CheckPlugin targets are user-defined GVRs (spec.targetResource), so the
// controller needs read access to arbitrary resource types. Verbs are
// restricted to read-only (get, list, watch). If CheckPlugin CRDs are not
// deployed, this ClusterRole rule can be removed from generated RBAC.
// +kubebuilder:rbac:groups="*",resources="*",verbs=get;list;watch

func (r *CheckPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the CheckPlugin
	cp := &assistv1alpha1.CheckPlugin{}
	if err := r.Get(ctx, req.NamespacedName, cp); err != nil {
		if apierrors.IsNotFound(err) {
			// Object deleted — unregister handled by finalizer
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch CheckPlugin: %w", err)
	}

	registryKey := cp.Name

	// Handle deletion via finalizer
	if !cp.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(cp, checkPluginFinalizer) {
			log.Info("Unregistering plugin checker on deletion", "plugin", cp.Spec.DisplayName, "registryKey", registryKey)
			r.Registry.Unregister("plugin:" + registryKey)

			controllerutil.RemoveFinalizer(cp, checkPluginFinalizer)
			if err := r.Update(ctx, cp); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from CheckPlugin: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(cp, checkPluginFinalizer) {
		controllerutil.AddFinalizer(cp, checkPluginFinalizer)
		if err := r.Update(ctx, cp); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer to CheckPlugin: %w", err)
		}
	}

	// Compile and register the plugin checker
	pc, err := plugin.NewPluginChecker(registryKey, cp.Spec)
	if err != nil {
		log.Error(err, "Failed to compile plugin checker", "plugin", cp.Spec.DisplayName, "registryKey", registryKey)
		return r.updateStatus(ctx, cp, false, err.Error())
	}

	r.Registry.Replace(pc)
	log.Info("Registered plugin checker", "plugin", cp.Spec.DisplayName, "registryKey", registryKey, "rules", len(cp.Spec.Rules))

	return r.updateStatus(ctx, cp, true, "")
}

// updateStatus patches the CheckPlugin status fields.
func (r *CheckPluginReconciler) updateStatus(ctx context.Context, cp *assistv1alpha1.CheckPlugin, ready bool, errMsg string) (ctrl.Result, error) {
	now := metav1.Now()
	cp.Status.Ready = ready
	cp.Status.Error = errMsg
	cp.Status.LastUpdated = &now

	if err := r.Status().Update(ctx, cp); err != nil {
		checkPluginLog.Error(err, "Failed to update CheckPlugin status", "plugin", cp.Spec.DisplayName)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CheckPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&assistv1alpha1.CheckPlugin{}).
		Named("checkplugin").
		Complete(r)
}
