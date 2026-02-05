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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/checker/workload"
)

var _ = Describe("TeamHealthRequest Controller", func() {

	Context("When reconciling a resource that does not exist", func() {
		It("should return without error", func() {
			registry := checker.NewRegistry()
			reconciler := &TeamHealthRequestReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "nonexistent-health",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})

	Context("When reconciling a completed resource", func() {
		const resourceName = "completed-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.TeamHealthPhaseCompleted
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())
		})

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				Expect(k8sClient.Delete(ctx, hr)).To(Succeed())
			}
		})

		It("should skip reconciliation for completed resources", func() {
			registry := checker.NewRegistry()
			reconciler := &TeamHealthRequestReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseCompleted))
		})
	})

	Context("When reconciling a failed resource", func() {
		const resourceName = "failed-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.TeamHealthPhaseFailed
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())
		})

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				Expect(k8sClient.Delete(ctx, hr)).To(Succeed())
			}
		})

		It("should skip reconciliation for failed resources", func() {
			registry := checker.NewRegistry()
			reconciler := &TeamHealthRequestReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})

	Context("When running with currentNamespaceOnly scope", func() {
		const resourceName = "current-ns-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Scope: assistv1alpha1.ScopeConfig{
						CurrentNamespaceOnly: true,
					},
					Checks: []assistv1alpha1.CheckerName{assistv1alpha1.CheckerWorkloads},
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())
		})

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				Expect(k8sClient.Delete(ctx, hr)).To(Succeed())
			}
		})

		It("should run checkers against the current namespace", func() {
			registry := checker.NewRegistry()
			registry.MustRegister(workload.New())

			reconciler := &TeamHealthRequestReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseCompleted))
			Expect(fetched.Status.NamespacesChecked).To(ContainElement("default"))
			Expect(fetched.Status.Results).To(HaveKey("workloads"))
			Expect(fetched.Status.LastCheckTime).NotTo(BeNil())

			// Check conditions
			var nsResolved, checkersCompleted, complete bool
			for _, cond := range fetched.Status.Conditions {
				switch cond.Type {
				case assistv1alpha1.TeamHealthConditionNamespacesResolved:
					nsResolved = cond.Status == metav1.ConditionTrue
				case assistv1alpha1.TeamHealthConditionCheckersCompleted:
					checkersCompleted = cond.Status == metav1.ConditionTrue
				case assistv1alpha1.TeamHealthConditionComplete:
					complete = cond.Status == metav1.ConditionTrue
				}
			}
			Expect(nsResolved).To(BeTrue(), "NamespacesResolved condition should be True")
			Expect(checkersCompleted).To(BeTrue(), "CheckersCompleted condition should be True")
			Expect(complete).To(BeTrue(), "Complete condition should be True")
		})
	})

	Context("When running with explicit namespaces", func() {
		const resourceName = "explicit-ns-health"
		const testNs = "test-health-ns"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			// Create a test namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: testNs},
			}
			err := k8sClient.Create(ctx, ns)
			if err != nil {
				// Namespace may already exist
				_ = err
			}

			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Scope: assistv1alpha1.ScopeConfig{
						Namespaces: []string{"default", testNs},
					},
					Checks: []assistv1alpha1.CheckerName{assistv1alpha1.CheckerWorkloads},
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())
		})

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				Expect(k8sClient.Delete(ctx, hr)).To(Succeed())
			}
		})

		It("should check the specified namespaces", func() {
			registry := checker.NewRegistry()
			registry.MustRegister(workload.New())

			reconciler := &TeamHealthRequestReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseCompleted))
			Expect(fetched.Status.NamespacesChecked).To(ContainElement("default"))
			Expect(fetched.Status.NamespacesChecked).To(ContainElement(testNs))
		})
	})

	Context("When running with namespace label selector", func() {
		const resourceName = "selector-ns-health"
		const labeledNs = "labeled-ns-for-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: labeledNs,
					Labels: map[string]string{
						"team": "platform",
					},
				},
			}
			err := k8sClient.Create(ctx, ns)
			if err != nil {
				_ = err
			}

			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Scope: assistv1alpha1.ScopeConfig{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"team": "platform"},
						},
					},
					Checks: []assistv1alpha1.CheckerName{assistv1alpha1.CheckerWorkloads},
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())
		})

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				Expect(k8sClient.Delete(ctx, hr)).To(Succeed())
			}
		})

		It("should resolve namespaces by label selector", func() {
			registry := checker.NewRegistry()
			registry.MustRegister(workload.New())

			reconciler := &TeamHealthRequestReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseCompleted))
			Expect(fetched.Status.NamespacesChecked).To(ContainElement(labeledNs))
		})
	})

	Context("When running with default (empty) scope", func() {
		const resourceName = "default-scope-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					// Empty scope - should default to current namespace
					Checks: []assistv1alpha1.CheckerName{assistv1alpha1.CheckerWorkloads},
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())
		})

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				Expect(k8sClient.Delete(ctx, hr)).To(Succeed())
			}
		})

		It("should default to the request's namespace", func() {
			registry := checker.NewRegistry()
			registry.MustRegister(workload.New())

			reconciler := &TeamHealthRequestReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseCompleted))
			Expect(fetched.Status.NamespacesChecked).To(Equal([]string{"default"}))
		})
	})

	Context("When running with workload config", func() {
		const resourceName = "config-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Scope: assistv1alpha1.ScopeConfig{
						CurrentNamespaceOnly: true,
					},
					Checks: []assistv1alpha1.CheckerName{assistv1alpha1.CheckerWorkloads},
					Config: assistv1alpha1.CheckerConfig{
						Workloads: &assistv1alpha1.WorkloadCheckConfig{
							RestartThreshold: 10,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())
		})

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				Expect(k8sClient.Delete(ctx, hr)).To(Succeed())
			}
		})

		It("should pass configuration to checkers", func() {
			registry := checker.NewRegistry()
			registry.MustRegister(workload.New())

			reconciler := &TeamHealthRequestReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseCompleted))
		})
	})

	Context("When running with all default checkers", func() {
		const resourceName = "all-checkers-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Scope: assistv1alpha1.ScopeConfig{
						CurrentNamespaceOnly: true,
					},
					// Empty checks means run all registered
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())
		})

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				Expect(k8sClient.Delete(ctx, hr)).To(Succeed())
			}
		})

		It("should run all registered checkers when no checks specified", func() {
			registry := checker.NewRegistry()
			registry.MustRegister(workload.New())

			reconciler := &TeamHealthRequestReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Registry: registry,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseCompleted))
			Expect(fetched.Status.Results).To(HaveKey("workloads"))
		})
	})

	Context("Helper function tests", func() {
		It("generateSummary should handle zero issues", func() {
			r := &TeamHealthRequestReconciler{}
			summary := r.generateSummary(5, 0, 0, 0)
			Expect(summary).To(ContainSubstring("5 resource(s) healthy"))
		})

		It("generateSummary should handle critical issues", func() {
			r := &TeamHealthRequestReconciler{}
			summary := r.generateSummary(3, 5, 2, 3)
			Expect(summary).To(ContainSubstring("2 critical"))
			Expect(summary).To(ContainSubstring("3 warning"))
		})

		It("generateSummary should handle warnings only", func() {
			r := &TeamHealthRequestReconciler{}
			summary := r.generateSummary(3, 2, 0, 2)
			Expect(summary).To(ContainSubstring("2 warning"))
			Expect(summary).NotTo(ContainSubstring("critical"))
		})

		It("getCheckerNames should return all names when checks empty", func() {
			registry := checker.NewRegistry()
			registry.MustRegister(workload.New())

			r := &TeamHealthRequestReconciler{Registry: registry}
			names := r.getCheckerNames(nil)
			Expect(names).To(ContainElement("workloads"))
		})

		It("getCheckerNames should return specified names", func() {
			registry := checker.NewRegistry()
			r := &TeamHealthRequestReconciler{Registry: registry}
			names := r.getCheckerNames([]assistv1alpha1.CheckerName{
				assistv1alpha1.CheckerWorkloads,
				assistv1alpha1.CheckerSecrets,
			})
			Expect(names).To(ConsistOf("workloads", "secrets"))
		})

		It("buildCheckerConfig should extract workload config", func() {
			r := &TeamHealthRequestReconciler{}
			cfg := r.buildCheckerConfig(assistv1alpha1.CheckerConfig{
				Workloads: &assistv1alpha1.WorkloadCheckConfig{
					RestartThreshold: 10,
				},
			})
			Expect(cfg).To(HaveKey(workload.ConfigRestartThreshold))
			Expect(cfg[workload.ConfigRestartThreshold]).To(Equal(10))
		})

		It("buildCheckerConfig should return empty config for nil workloads", func() {
			r := &TeamHealthRequestReconciler{}
			cfg := r.buildCheckerConfig(assistv1alpha1.CheckerConfig{})
			Expect(cfg).To(BeEmpty())
		})
	})
})
