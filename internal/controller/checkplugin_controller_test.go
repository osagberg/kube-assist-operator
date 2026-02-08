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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/checker"
)

var _ = Describe("CheckPlugin Controller", func() {

	Context("When reconciling a resource that does not exist", func() {
		It("should return without error for a not-found resource", func() {
			registry := checker.NewRegistry()
			reconciler := &CheckPluginReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "nonexistent-checkplugin",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})

	Context("When reconciling a valid CheckPlugin", func() {
		const resourceName = "valid-plugin"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			cp := &assistv1alpha1.CheckPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.CheckPluginSpec{
					DisplayName: "Valid Plugin",
					Description: "A test plugin with valid CEL rules",
					TargetResource: assistv1alpha1.TargetGVR{
						Group:    "apps",
						Version:  "v1",
						Resource: "deployments",
						Kind:     "Deployment",
					},
					Rules: []assistv1alpha1.CheckRule{{
						Name:      "replicas-zero",
						Condition: "object.spec.replicas == 0",
						Severity:  "Warning",
						Message:   "Deployment has zero replicas",
					}},
				},
			}
			Expect(k8sClient.Create(ctx, cp)).To(Succeed())
		})

		AfterEach(func() {
			cp := &assistv1alpha1.CheckPlugin{}
			if err := k8sClient.Get(ctx, nn, cp); err == nil {
				// Remove finalizer so deletion can proceed
				if controllerutil.ContainsFinalizer(cp, "assist.cluster.local/checkplugin-finalizer") {
					controllerutil.RemoveFinalizer(cp, "assist.cluster.local/checkplugin-finalizer")
					_ = k8sClient.Update(ctx, cp)
				}
				Expect(k8sClient.Delete(ctx, cp)).To(Succeed())
			}
		})

		It("should set Ready=True, add finalizer, and register plugin in registry", func() {
			registry := checker.NewRegistry()
			reconciler := &CheckPluginReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Verify status
			fetched := &assistv1alpha1.CheckPlugin{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Ready).To(BeTrue(), "Ready should be True for valid CEL rules")
			Expect(fetched.Status.Error).To(BeEmpty())
			Expect(fetched.Status.LastUpdated).NotTo(BeNil())

			// Verify finalizer was added
			Expect(controllerutil.ContainsFinalizer(fetched, "assist.cluster.local/checkplugin-finalizer")).To(BeTrue())

			// Verify plugin registered in registry with "plugin:" prefix + metadata.name
			registryKey := "plugin:" + resourceName
			_, found := registry.Get(registryKey)
			Expect(found).To(BeTrue(), "plugin should be registered in registry with key %q", registryKey)
		})
	})

	Context("When reconciling a CheckPlugin with invalid CEL expression", func() {
		const resourceName = "invalid-cel-plugin"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			cp := &assistv1alpha1.CheckPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.CheckPluginSpec{
					DisplayName: "Invalid Plugin",
					TargetResource: assistv1alpha1.TargetGVR{
						Group:    "apps",
						Version:  "v1",
						Resource: "deployments",
						Kind:     "Deployment",
					},
					Rules: []assistv1alpha1.CheckRule{{
						Name:      "bad-rule",
						Condition: "this is not valid CEL !!!",
						Severity:  "Warning",
						Message:   "Should not compile",
					}},
				},
			}
			Expect(k8sClient.Create(ctx, cp)).To(Succeed())
		})

		AfterEach(func() {
			cp := &assistv1alpha1.CheckPlugin{}
			if err := k8sClient.Get(ctx, nn, cp); err == nil {
				if controllerutil.ContainsFinalizer(cp, "assist.cluster.local/checkplugin-finalizer") {
					controllerutil.RemoveFinalizer(cp, "assist.cluster.local/checkplugin-finalizer")
					_ = k8sClient.Update(ctx, cp)
				}
				Expect(k8sClient.Delete(ctx, cp)).To(Succeed())
			}
		})

		It("should set Ready=False and populate error", func() {
			registry := checker.NewRegistry()
			reconciler := &CheckPluginReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			// First reconcile adds the finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile compiles and sets status
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.CheckPlugin{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Ready).To(BeFalse(), "Ready should be False for invalid CEL")
			Expect(fetched.Status.Error).NotTo(BeEmpty(), "Error should contain compilation error")

			// Verify plugin is NOT registered in registry
			registryKey := "plugin:" + resourceName
			_, found := registry.Get(registryKey)
			Expect(found).To(BeFalse(), "invalid plugin should not be registered")
		})
	})

	Context("When updating CheckPlugin rules", func() {
		const resourceName = "update-rules-plugin"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			cp := &assistv1alpha1.CheckPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.CheckPluginSpec{
					DisplayName: "Update Plugin",
					TargetResource: assistv1alpha1.TargetGVR{
						Group:    "apps",
						Version:  "v1",
						Resource: "deployments",
						Kind:     "Deployment",
					},
					Rules: []assistv1alpha1.CheckRule{{
						Name:      "original-rule",
						Condition: "object.spec.replicas == 0",
						Severity:  "Warning",
						Message:   "Original message",
					}},
				},
			}
			Expect(k8sClient.Create(ctx, cp)).To(Succeed())
		})

		AfterEach(func() {
			cp := &assistv1alpha1.CheckPlugin{}
			if err := k8sClient.Get(ctx, nn, cp); err == nil {
				if controllerutil.ContainsFinalizer(cp, "assist.cluster.local/checkplugin-finalizer") {
					controllerutil.RemoveFinalizer(cp, "assist.cluster.local/checkplugin-finalizer")
					_ = k8sClient.Update(ctx, cp)
				}
				Expect(k8sClient.Delete(ctx, cp)).To(Succeed())
			}
		})

		It("should update registry with new version and remain Ready=True", func() {
			registry := checker.NewRegistry()
			reconciler := &CheckPluginReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			// Initial reconcile (adds finalizer)
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile (compiles + registers)
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			registryKey := "plugin:" + resourceName
			_, found := registry.Get(registryKey)
			Expect(found).To(BeTrue(), "plugin should be registered after initial reconcile")

			// Update the spec with a new rule
			fetched := &assistv1alpha1.CheckPlugin{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Spec.Rules = []assistv1alpha1.CheckRule{{
				Name:      "updated-rule",
				Condition: "object.spec.replicas == 1",
				Severity:  "Info",
				Message:   "Updated message",
			}}
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			// Reconcile the update
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Verify still ready
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Ready).To(BeTrue(), "Ready should still be True after valid update")

			// Verify registry still has it (Replace was called)
			_, found = registry.Get(registryKey)
			Expect(found).To(BeTrue(), "plugin should still be in registry after update")
		})
	})

	Context("When deleting a CheckPlugin", func() {
		const resourceName = "delete-plugin"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		It("should unregister plugin from registry via finalizer", func() {
			registry := checker.NewRegistry()
			reconciler := &CheckPluginReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			cp := &assistv1alpha1.CheckPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.CheckPluginSpec{
					DisplayName: "Delete Plugin",
					TargetResource: assistv1alpha1.TargetGVR{
						Group:    "apps",
						Version:  "v1",
						Resource: "deployments",
						Kind:     "Deployment",
					},
					Rules: []assistv1alpha1.CheckRule{{
						Name:      "some-rule",
						Condition: "object.spec.replicas == 0",
						Severity:  "Warning",
						Message:   "Zero replicas",
					}},
				},
			}
			Expect(k8sClient.Create(ctx, cp)).To(Succeed())

			// Reconcile to add finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Reconcile to compile and register
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			registryKey := "plugin:" + resourceName
			_, found := registry.Get(registryKey)
			Expect(found).To(BeTrue(), "plugin should be registered before deletion")

			// Delete the CR
			Expect(k8sClient.Delete(ctx, cp)).To(Succeed())

			// Reconcile the deletion — finalizer should unregister
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Verify plugin is unregistered
			_, found = registry.Get(registryKey)
			Expect(found).To(BeFalse(), "plugin should be unregistered after deletion")
		})
	})

	Context("Registry key verification", func() {
		It("should use 'plugin:' + metadata.name as the registry key, not displayName", func() {
			const resourceName = "key-test-plugin"
			ctx := context.Background()
			nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

			registry := checker.NewRegistry()
			reconciler := &CheckPluginReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			cp := &assistv1alpha1.CheckPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.CheckPluginSpec{
					DisplayName: "Completely Different Display Name",
					TargetResource: assistv1alpha1.TargetGVR{
						Group:    "apps",
						Version:  "v1",
						Resource: "deployments",
						Kind:     "Deployment",
					},
					Rules: []assistv1alpha1.CheckRule{{
						Name:      "test-rule",
						Condition: "object.spec.replicas == 0",
						Severity:  "Warning",
						Message:   "Test message",
					}},
				},
			}
			Expect(k8sClient.Create(ctx, cp)).To(Succeed())

			defer func() {
				fetched := &assistv1alpha1.CheckPlugin{}
				if err := k8sClient.Get(ctx, nn, fetched); err == nil {
					if controllerutil.ContainsFinalizer(fetched, "assist.cluster.local/checkplugin-finalizer") {
						controllerutil.RemoveFinalizer(fetched, "assist.cluster.local/checkplugin-finalizer")
						_ = k8sClient.Update(ctx, fetched)
					}
					_ = k8sClient.Delete(ctx, fetched)
				}
			}()

			// Reconcile (add finalizer)
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Reconcile (compile + register)
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Key should be "plugin:" + metadata.name
			correctKey := "plugin:" + resourceName
			_, found := registry.Get(correctKey)
			Expect(found).To(BeTrue(), "registry key should be %q", correctKey)

			// Key should NOT be "plugin:" + displayName
			wrongKey := "plugin:Completely Different Display Name"
			_, found = registry.Get(wrongKey)
			Expect(found).To(BeFalse(), "registry key should NOT use displayName")
		})
	})
})
