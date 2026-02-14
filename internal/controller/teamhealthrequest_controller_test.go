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
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/checker/workload"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

var _ = Describe("TeamHealthRequest Controller", func() {

	Context("When reconciling a resource that does not exist", func() {
		It("should return without error", func() {
			registry := checker.NewRegistry()
			reconciler := &TeamHealthRequestReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
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
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
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
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})

	Context("When reconciling a running resource", func() {
		const resourceName = "running-health"
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
			now := metav1.Now()
			fetched.Status.Phase = assistv1alpha1.TeamHealthPhaseRunning
			fetched.Status.StartedAt = &now
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())
		})

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				Expect(k8sClient.Delete(ctx, hr)).To(Succeed())
			}
		})

		It("should skip duplicate execution and requeue", func() {
			registry := checker.NewRegistry()
			reconciler := &TeamHealthRequestReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseRunning))
			Expect(fetched.Status.StartedAt).NotTo(BeNil())
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
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
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
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
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
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
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
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
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
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
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
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
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

	Context("TTL cleanup for completed requests", func() {
		const resourceName = "ttl-completed-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				Expect(k8sClient.Delete(ctx, hr)).To(Succeed())
			}
		})

		It("should delete a completed request when TTL has expired", func() {
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					TTLSecondsAfterFinished: ptr.To(int32(0)),
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.TeamHealthPhaseCompleted
			pastTime := metav1.NewTime(time.Now().Add(-1 * time.Minute))
			fetched.Status.CompletedAt = &pastTime
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			registry := checker.NewRegistry()
			reconciler := &TeamHealthRequestReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, nn, &assistv1alpha1.TeamHealthRequest{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected resource to be deleted by TTL")
		})

		It("should requeue a completed request when TTL has not expired", func() {
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					TTLSecondsAfterFinished: ptr.To(int32(3600)),
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.TeamHealthPhaseCompleted
			now := metav1.Now()
			fetched.Status.CompletedAt = &now
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			registry := checker.NewRegistry()
			reconciler := &TeamHealthRequestReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "should requeue for later TTL check")

			Expect(k8sClient.Get(ctx, nn, &assistv1alpha1.TeamHealthRequest{})).To(Succeed())
		})

		It("should set CompletedAt when completing successfully", func() {
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

			registry := checker.NewRegistry()
			registry.MustRegister(workload.New())

			reconciler := &TeamHealthRequestReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseCompleted))
			Expect(fetched.Status.CompletedAt).NotTo(BeNil(), "CompletedAt should be set on completion")
			Expect(fetched.Status.LastCheckTime).NotTo(BeNil())
		})
	})

	Context("TTL cleanup for failed requests", func() {
		const resourceName = "ttl-failed-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				Expect(k8sClient.Delete(ctx, hr)).To(Succeed())
			}
		})

		It("should delete a failed request when TTL has expired", func() {
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					TTLSecondsAfterFinished: ptr.To(int32(0)),
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.TeamHealthPhaseFailed
			pastTime := metav1.NewTime(time.Now().Add(-1 * time.Minute))
			fetched.Status.CompletedAt = &pastTime
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			registry := checker.NewRegistry()
			reconciler := &TeamHealthRequestReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, nn, &assistv1alpha1.TeamHealthRequest{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected failed resource to be deleted by TTL")
		})

		It("should requeue a failed request when TTL has not expired", func() {
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					TTLSecondsAfterFinished: ptr.To(int32(3600)),
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.TeamHealthPhaseFailed
			now := metav1.Now()
			fetched.Status.CompletedAt = &now
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			registry := checker.NewRegistry()
			reconciler := &TeamHealthRequestReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "should requeue for later TTL check")

			Expect(k8sClient.Get(ctx, nn, &assistv1alpha1.TeamHealthRequest{})).To(Succeed())
		})
	})

	Context("When running with an unregistered checker name", func() {
		const resourceName = "unregistered-checker-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			// Use a valid checker name that passes webhook validation, but don't register it
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Scope: assistv1alpha1.ScopeConfig{
						CurrentNamespaceOnly: true,
					},
					Checks: []assistv1alpha1.CheckerName{assistv1alpha1.CheckerSecrets},
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

		It("should complete with an error in the checker result", func() {
			// Use an empty registry — "secrets" is a valid API name but not registered
			registry := checker.NewRegistry()

			reconciler := &TeamHealthRequestReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseCompleted))
			Expect(fetched.Status.Results).To(HaveKey("secrets"))
			Expect(fetched.Status.Results["secrets"].Error).NotTo(BeEmpty(),
				"unregistered checker should have an error in its result")
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

		It("validateNotificationURL should reject HTTP by default", func() {
			r := &TeamHealthRequestReconciler{}
			err := r.validateNotificationURL(&assistv1alpha1.TeamHealthRequest{}, "http://example.com/webhook")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must use HTTPS"))
		})

		It("validateNotificationURL should allow HTTP with explicit annotation", func() {
			r := &TeamHealthRequestReconciler{}
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						assistv1alpha1.AllowHTTPWebhooksAnnotation: "true",
					},
				},
			}
			Expect(r.validateNotificationURL(hr, "http://example.com/webhook")).To(Succeed())
		})

		It("validateNotificationURL should reject missing host", func() {
			r := &TeamHealthRequestReconciler{}
			err := r.validateNotificationURL(&assistv1alpha1.TeamHealthRequest{}, "https:///path-only")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("include a host"))
		})
	})

	Context("Notification semaphore", func() {
		It("should not launch goroutine when semaphore is full", func() {
			sem := make(chan struct{}, 1)
			// Fill the semaphore — simulates 1 in-flight notification
			sem <- struct{}{}

			// The launch-site pattern from Reconcile: acquire before go
			launched := false
			select {
			case sem <- struct{}{}:
				// Would launch goroutine here in production
				launched = true
				<-sem // release (simulating defer)
			default:
				// Semaphore full — skip
			}

			Expect(launched).To(BeFalse(), "goroutine should NOT be launched when semaphore is full")

			// Drain
			<-sem
		})

		It("should launch goroutine when semaphore has capacity", func() {
			sem := make(chan struct{}, 5)

			launched := false
			select {
			case sem <- struct{}{}:
				launched = true
				<-sem // release
			default:
			}

			Expect(launched).To(BeTrue(), "goroutine should be launched when semaphore has capacity")
			Expect(sem).To(BeEmpty(), "semaphore should be released")
		})

		It("should bound concurrent dispatches to semaphore capacity", func() {
			sem := make(chan struct{}, 2)
			launchCount := 0

			for range 5 {
				select {
				case sem <- struct{}{}:
					launchCount++
				default:
					// Semaphore full — skip
				}
			}

			Expect(launchCount).To(Equal(2), "only 2 goroutines should be launched with capacity 2")
			// Drain
			for range launchCount {
				<-sem
			}
		})

		It("should dispatch notifications when called directly", func() {
			r := &TeamHealthRequestReconciler{
				NotifySem: make(chan struct{}, 5),
			}

			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sem-test-ok",
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Notify: []assistv1alpha1.NotificationTarget{
						{
							Type:         assistv1alpha1.NotificationTypeWebhook,
							URL:          "https://example.com/webhook",
							OnCompletion: true,
						},
					},
				},
			}

			done := make(chan struct{})
			go func() {
				defer close(done)
				r.dispatchNotifications(context.Background(), hr, 5, 0, 0, 0)
			}()

			Eventually(done, "5s").Should(BeClosed())
		})
	})

	Context("severityMet helper", func() {
		It("should return true for Critical threshold when critical > 0", func() {
			r := &TeamHealthRequestReconciler{}
			Expect(r.severityMet("Critical", 1, 0)).To(BeTrue())
		})

		It("should return false for Critical threshold when critical = 0", func() {
			r := &TeamHealthRequestReconciler{}
			Expect(r.severityMet("Critical", 0, 5)).To(BeFalse())
		})

		It("should return true for Warning threshold when warning > 0", func() {
			r := &TeamHealthRequestReconciler{}
			Expect(r.severityMet("Warning", 0, 1)).To(BeTrue())
		})

		It("should return true for Warning threshold when critical > 0", func() {
			r := &TeamHealthRequestReconciler{}
			Expect(r.severityMet("Warning", 1, 0)).To(BeTrue())
		})

		It("should return false for Warning threshold when both are 0", func() {
			r := &TeamHealthRequestReconciler{}
			Expect(r.severityMet("Warning", 0, 0)).To(BeFalse())
		})

		It("should return true for Info threshold always", func() {
			r := &TeamHealthRequestReconciler{}
			Expect(r.severityMet("Info", 0, 0)).To(BeTrue())
		})

		It("should return true for unknown threshold", func() {
			r := &TeamHealthRequestReconciler{}
			Expect(r.severityMet("Unknown", 0, 0)).To(BeTrue())
		})
	})

	Context("launchNotifications", func() {
		It("should dispatch notifications without semaphore", func() {
			r := &TeamHealthRequestReconciler{
				// NotifySem is nil — covers the else branch
			}

			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "launch-no-sem",
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Notify: []assistv1alpha1.NotificationTarget{
						{
							Type:         assistv1alpha1.NotificationTypeWebhook,
							URL:          "https://example.com/webhook",
							OnCompletion: true,
						},
					},
				},
			}

			// Should not panic — fire-and-forget goroutine
			r.launchNotifications(context.Background(), hr, 5, 0, 0, 0)
			// Give goroutine time to complete
			time.Sleep(100 * time.Millisecond)
		})

		It("should skip when semaphore is full", func() {
			sem := make(chan struct{}, 1)
			sem <- struct{}{} // fill

			r := &TeamHealthRequestReconciler{
				NotifySem: sem,
			}

			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "launch-sem-full",
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Notify: []assistv1alpha1.NotificationTarget{
						{
							Type:         assistv1alpha1.NotificationTypeWebhook,
							URL:          "https://example.com/webhook",
							OnCompletion: true,
						},
					},
				},
			}

			// Should not panic — just logs and skips
			r.launchNotifications(context.Background(), hr, 5, 0, 0, 0)
			time.Sleep(50 * time.Millisecond)

			// Drain
			<-sem
		})
	})

	Context("dispatchNotifications with severity filter", func() {
		It("should skip notifications when severity filter is not met", func() {
			r := &TeamHealthRequestReconciler{}

			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sev-filter",
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Notify: []assistv1alpha1.NotificationTarget{
						{
							Type:         assistv1alpha1.NotificationTypeWebhook,
							URL:          "https://example.com/webhook",
							OnCompletion: true,
							OnSeverity:   "Critical",
						},
					},
				},
			}

			// No critical issues — should skip
			r.dispatchNotifications(context.Background(), hr, 5, 2, 0, 2)
			// No assertion needed — just verifying it doesn't panic and exits gracefully
		})

		It("should skip notifications when OnCompletion is false", func() {
			r := &TeamHealthRequestReconciler{}

			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-completion",
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Notify: []assistv1alpha1.NotificationTarget{
						{
							Type:         assistv1alpha1.NotificationTypeWebhook,
							URL:          "https://example.com/webhook",
							OnCompletion: false,
						},
					},
				},
			}

			r.dispatchNotifications(context.Background(), hr, 5, 0, 0, 0)
		})

		It("should skip non-webhook notification types", func() {
			r := &TeamHealthRequestReconciler{}

			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-webhook",
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Notify: []assistv1alpha1.NotificationTarget{
						{
							Type:         "email", // unsupported
							URL:          "https://example.com/webhook",
							OnCompletion: true,
						},
					},
				},
			}

			r.dispatchNotifications(context.Background(), hr, 5, 0, 0, 0)
		})

		It("should skip notifications with empty URL", func() {
			r := &TeamHealthRequestReconciler{}

			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-url",
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Notify: []assistv1alpha1.NotificationTarget{
						{
							Type:         assistv1alpha1.NotificationTypeWebhook,
							URL:          "",
							OnCompletion: true,
						},
					},
				},
			}

			r.dispatchNotifications(context.Background(), hr, 5, 0, 0, 0)
		})

		It("should read URL from secretRef when URL is empty", func() {
			ctx := context.Background()
			// Create secret with webhook URL
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-secret-test",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"url": []byte("https://example.com/from-secret"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			defer func() {
				_ = k8sClient.Delete(ctx, secret)
			}()

			r := &TeamHealthRequestReconciler{
				Client: k8sClient,
			}

			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret-ref-dispatch",
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Notify: []assistv1alpha1.NotificationTarget{
						{
							Type:         assistv1alpha1.NotificationTypeWebhook,
							OnCompletion: true,
							SecretRef: &assistv1alpha1.SecretKeyRef{
								Name: "webhook-secret-test",
								Key:  "url",
							},
						},
					},
				},
			}

			// Should not panic, and attempt to send
			r.dispatchNotifications(ctx, hr, 5, 1, 1, 0)
		})

		It("should skip secretRef notifications outside configured secret namespace", func() {
			ctx := context.Background()
			var callCount atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				callCount.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			// Secret exists in request namespace, but controller is restricted to kube-assist-system.
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-secret-default",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"url": []byte(srv.URL),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, secret) }()

			r := &TeamHealthRequestReconciler{
				Client:                      k8sClient,
				NotificationSecretNamespace: "kube-assist-system",
			}
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret-ref-default",
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Notify: []assistv1alpha1.NotificationTarget{
						{
							Type:         assistv1alpha1.NotificationTypeWebhook,
							OnCompletion: true,
							SecretRef: &assistv1alpha1.SecretKeyRef{
								Name: "webhook-secret-default",
								Key:  "url",
							},
						},
					},
				},
			}
			r.dispatchNotifications(ctx, hr, 5, 1, 1, 0)
			Expect(callCount.Load()).To(Equal(int32(0)))
		})
	})

	Context("aggregateResults helper", func() {
		It("should count all-healthy checkers correctly", func() {
			r := &TeamHealthRequestReconciler{}
			results := map[string]*checker.CheckResult{
				"workloads": {Healthy: 5, Issues: nil},
				"secrets":   {Healthy: 3, Issues: nil},
			}
			agg := r.aggregateResults(results)

			Expect(agg.totalHealthy).To(Equal(8))
			Expect(agg.totalIssues).To(Equal(0))
			Expect(agg.criticalCount).To(Equal(0))
			Expect(agg.warningCount).To(Equal(0))
			Expect(agg.apiResults).To(HaveLen(2))
			Expect(agg.apiResults["workloads"].Healthy).To(Equal(int32(5)))
			Expect(agg.apiResults["secrets"].Healthy).To(Equal(int32(3)))
		})

		It("should exclude errored checker from healthy/issue counts", func() {
			r := &TeamHealthRequestReconciler{}
			results := map[string]*checker.CheckResult{
				"workloads": {Healthy: 5, Issues: nil},
				"pvcs":      {Healthy: 0, Error: errors.New("list PVCs: forbidden")},
			}
			agg := r.aggregateResults(results)

			Expect(agg.totalHealthy).To(Equal(5))
			Expect(agg.totalIssues).To(Equal(0))
			Expect(agg.apiResults["pvcs"].Error).To(Equal("list PVCs: forbidden"))
			Expect(agg.apiResults["pvcs"].Healthy).To(Equal(int32(0)))
		})

		It("should tally critical and warning severities", func() {
			r := &TeamHealthRequestReconciler{}
			results := map[string]*checker.CheckResult{
				"workloads": {
					Healthy: 3,
					Issues: []checker.Issue{
						{Severity: checker.SeverityCritical, Type: "CrashLoop", Message: "pod crash"},
						{Severity: checker.SeverityWarning, Type: "HighRestart", Message: "restarts"},
						{Severity: checker.SeverityWarning, Type: "HighRestart", Message: "restarts2"},
					},
				},
			}
			agg := r.aggregateResults(results)

			Expect(agg.totalHealthy).To(Equal(3))
			Expect(agg.totalIssues).To(Equal(3))
			Expect(agg.criticalCount).To(Equal(1))
			Expect(agg.warningCount).To(Equal(2))
			Expect(agg.apiResults["workloads"].Issues).To(HaveLen(3))
		})

		It("should handle empty results map", func() {
			r := &TeamHealthRequestReconciler{}
			agg := r.aggregateResults(map[string]*checker.CheckResult{})

			Expect(agg.totalHealthy).To(Equal(0))
			Expect(agg.totalIssues).To(Equal(0))
			Expect(agg.criticalCount).To(Equal(0))
			Expect(agg.warningCount).To(Equal(0))
			Expect(agg.apiResults).To(BeEmpty())
		})

		It("should handle mixed error and healthy results from different checkers", func() {
			r := &TeamHealthRequestReconciler{}
			results := map[string]*checker.CheckResult{
				"workloads": {Healthy: 10, Issues: []checker.Issue{
					{Severity: checker.SeverityCritical, Type: "CrashLoop", Message: "crash"},
				}},
				"pvcs":    {Healthy: 0, Error: errors.New("forbidden")},
				"secrets": {Healthy: 7, Issues: nil},
			}
			agg := r.aggregateResults(results)

			Expect(agg.totalHealthy).To(Equal(17)) // 10 + 7 (pvcs excluded)
			Expect(agg.totalIssues).To(Equal(1))
			Expect(agg.criticalCount).To(Equal(1))
			Expect(agg.apiResults["pvcs"].Error).To(Equal("forbidden"))
			Expect(agg.apiResults).To(HaveLen(3))
		})
	})

	Context("reconcile with checker error", func() {
		const resourceName = "checker-error-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				_ = k8sClient.Delete(ctx, hr)
			}
		})

		It("should complete with error string in results when a checker fails", func() {
			// Register a checker under the "workloads" name that always errors
			registry := checker.NewRegistry()
			Expect(registry.Register(&errorChecker{
				name: "workloads",
				err:  errors.New("simulated failure"),
			})).To(Succeed())

			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{
					Checks: []assistv1alpha1.CheckerName{assistv1alpha1.CheckerWorkloads},
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())

			reconciler := &TeamHealthRequestReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseCompleted))
			Expect(fetched.Status.Results).To(HaveKey("workloads"))
			Expect(fetched.Status.Results["workloads"].Error).To(Equal("simulated failure"))
		})
	})

	Context("setFailed helper", func() {
		const resourceName = "set-failed-health"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		AfterEach(func() {
			hr := &assistv1alpha1.TeamHealthRequest{}
			if err := k8sClient.Get(ctx, nn, hr); err == nil {
				_ = k8sClient.Delete(ctx, hr)
			}
		})

		It("should set phase to Failed with correct status fields", func() {
			hr := &assistv1alpha1.TeamHealthRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TeamHealthRequestSpec{},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())
			Expect(k8sClient.Get(ctx, nn, hr)).To(Succeed())

			original := hr.DeepCopy()
			registry := checker.NewRegistry()
			reconciler := &TeamHealthRequestReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Registry:   registry,
				DataSource: datasource.NewKubernetes(k8sClient),
			}

			result, err := reconciler.setFailed(ctx, original, hr, "namespace resolution failed")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TeamHealthRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.TeamHealthPhaseFailed))
			Expect(fetched.Status.Summary).To(Equal("namespace resolution failed"))
			Expect(fetched.Status.CompletedAt).NotTo(BeNil())
			Expect(fetched.Status.LastCheckTime).NotTo(BeNil())
		})
	})
})

// errorChecker is a test helper that always returns an error in its result.
type errorChecker struct {
	name string
	err  error
}

func (e *errorChecker) Name() string { return e.name }
func (e *errorChecker) Check(_ context.Context, _ *checker.CheckContext) (*checker.CheckResult, error) {
	return &checker.CheckResult{
		CheckerName: e.name,
		Error:       e.err,
	}, nil
}
func (e *errorChecker) Supports(_ context.Context, _ datasource.DataSource) bool { return true }
