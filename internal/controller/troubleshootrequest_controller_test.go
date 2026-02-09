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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
)

var _ = Describe("TroubleshootRequest Controller", func() {

	Context("When reconciling a resource that does not exist", func() {
		It("should return without error for a not-found resource", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "nonexistent-resource",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})

	Context("When reconciling a completed resource", func() {
		const resourceName = "completed-tr"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: "some-deploy",
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			// Manually set status to Completed
			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.PhaseCompleted
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			err := k8sClient.Get(ctx, nn, tr)
			if err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
		})

		It("should skip reconciliation for completed resources", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Verify status is still Completed and not re-processed
			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseCompleted))
		})
	})

	Context("When reconciling a failed resource", func() {
		const resourceName = "failed-tr"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: "some-deploy",
					},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.PhaseFailed
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			err := k8sClient.Get(ctx, nn, tr)
			if err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
		})

		It("should skip reconciliation for failed resources", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})

	Context("When targeting a nonexistent Deployment", func() {
		const resourceName = "missing-target-tr"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: "nonexistent-deploy",
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			err := k8sClient.Get(ctx, nn, tr)
			if err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
		})

		It("should set status to Failed when target is not found", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseFailed))
			Expect(fetched.Status.Summary).To(ContainSubstring("Failed to find target"))
			Expect(fetched.Status.CompletedAt).NotTo(BeNil())
		})
	})

	Context("When diagnosing a healthy Deployment", func() {
		const resourceName = "healthy-deploy-tr"
		const deployName = "healthy-app"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			// Create a Deployment with matching labels
			replicas := int32(1)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployName,
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "healthy-app"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "healthy-app"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:latest",
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("256Mi"),
											corev1.ResourceCPU:    resource.MustParse("500m"),
										},
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("128Mi"),
											corev1.ResourceCPU:    resource.MustParse("100m"),
										},
									},
									LivenessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path: "/healthz",
												Port: intstr.FromInt(8080),
											},
										},
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path: "/readyz",
												Port: intstr.FromInt(8080),
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			// Create a matching Pod with healthy status
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "healthy-app-pod-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "healthy-app"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("256Mi"),
									corev1.ResourceCPU:    resource.MustParse("500m"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("128Mi"),
									corev1.ResourceCPU:    resource.MustParse("100m"),
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// Update pod status to Running
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "app",
						Ready: true,
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			// Create TroubleshootRequest
			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: deployName,
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
			deploy := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: "default"}, deploy); err == nil {
				Expect(k8sClient.Delete(ctx, deploy)).To(Succeed())
			}
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "healthy-app-pod-1", Namespace: "default"}, pod); err == nil {
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			}
		})

		It("should complete successfully with no issues for a healthy deployment", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseCompleted))
			Expect(fetched.Status.StartedAt).NotTo(BeNil())
			Expect(fetched.Status.CompletedAt).NotTo(BeNil())
			Expect(fetched.Status.Summary).NotTo(BeEmpty())

			// Check conditions
			var targetFound, diagnosed, complete bool
			for _, cond := range fetched.Status.Conditions {
				switch cond.Type {
				case assistv1alpha1.ConditionTargetFound:
					targetFound = cond.Status == metav1.ConditionTrue
				case assistv1alpha1.ConditionDiagnosed:
					diagnosed = cond.Status == metav1.ConditionTrue
				case assistv1alpha1.ConditionComplete:
					complete = cond.Status == metav1.ConditionTrue
				}
			}
			Expect(targetFound).To(BeTrue(), "TargetFound condition should be True")
			Expect(diagnosed).To(BeTrue(), "Diagnosed condition should be True")
			Expect(complete).To(BeTrue(), "Complete condition should be True")
		})
	})

	Context("When diagnosing a Deployment with unhealthy pods", func() {
		const resourceName = "unhealthy-deploy-tr"
		const deployName = "unhealthy-app"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			replicas := int32(1)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployName,
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "unhealthy-app"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "unhealthy-app"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "app", Image: "nginx:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unhealthy-app-pod-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "unhealthy-app"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:latest"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// Set pod to CrashLoopBackOff
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "app",
						Ready: false,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason:  "CrashLoopBackOff",
								Message: "back-off 5m0s restarting failed container",
							},
						},
						RestartCount: 10,
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: deployName,
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
			deploy := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: "default"}, deploy); err == nil {
				Expect(k8sClient.Delete(ctx, deploy)).To(Succeed())
			}
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "unhealthy-app-pod-1", Namespace: "default"}, pod); err == nil {
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			}
		})

		It("should detect issues and complete with findings", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseCompleted))
			Expect(fetched.Status.Issues).ToNot(BeEmpty())

			// Should find ContainerNotReady and HighRestartCount
			issueTypes := make(map[string]bool)
			for _, issue := range fetched.Status.Issues {
				issueTypes[issue.Type] = true
			}
			Expect(issueTypes["ContainerNotReady"]).To(BeTrue(), "Should detect ContainerNotReady")
			Expect(issueTypes["HighRestartCount"]).To(BeTrue(), "Should detect HighRestartCount")

			// Summary should indicate critical issues
			Expect(fetched.Status.Summary).To(ContainSubstring("critical"))
		})
	})

	Context("When targeting a Deployment with no pods", func() {
		const resourceName = "no-pods-tr"
		const deployName = "empty-deploy"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			replicas := int32(0)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployName,
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "empty-deploy"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "empty-deploy"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "app", Image: "nginx:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: deployName,
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
			deploy := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: "default"}, deploy); err == nil {
				Expect(k8sClient.Delete(ctx, deploy)).To(Succeed())
			}
		})

		It("should fail when no pods are found", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseFailed))
			Expect(fetched.Status.Summary).To(ContainSubstring("No pods found"))
		})
	})

	Context("When targeting a StatefulSet", func() {
		const resourceName = "sts-tr"
		const stsName = "test-sts"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			replicas := int32(1)
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stsName,
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test-sts"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test-sts"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "db", Image: "postgres:15"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, sts)).To(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts-0",
					Namespace: "default",
					Labels:    map[string]string{"app": "test-sts"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "db", Image: "postgres:15"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "db", Ready: true},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "StatefulSet",
						Name: stsName,
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
			sts := &appsv1.StatefulSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: stsName, Namespace: "default"}, sts); err == nil {
				Expect(k8sClient.Delete(ctx, sts)).To(Succeed())
			}
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-sts-0", Namespace: "default"}, pod); err == nil {
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			}
		})

		It("should reconcile StatefulSet targets", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseCompleted))
		})
	})

	Context("When targeting a DaemonSet", func() {
		const resourceName = "ds-tr"
		const dsName = "test-ds"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			ds := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName,
					Namespace: "default",
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test-ds"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test-ds"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "agent", Image: "busybox"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ds-node1",
					Namespace: "default",
					Labels:    map[string]string{"app": "test-ds"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "agent", Image: "busybox"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "agent", Ready: true},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "DaemonSet",
						Name: dsName,
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
			ds := &appsv1.DaemonSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: dsName, Namespace: "default"}, ds); err == nil {
				Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
			}
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-ds-node1", Namespace: "default"}, pod); err == nil {
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			}
		})

		It("should reconcile DaemonSet targets", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseCompleted))
		})
	})

	Context("When targeting a Pod directly", func() {
		const resourceName = "pod-tr"
		const podName = "direct-pod"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:latest"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "app", Ready: true},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Pod",
						Name: podName,
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: "default"}, pod); err == nil {
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			}
		})

		It("should reconcile Pod targets directly", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseCompleted))
		})
	})

	Context("When using the 'all' action", func() {
		const resourceName = "all-action-tr"
		const deployName = "all-action-deploy"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			replicas := int32(1)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployName,
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "all-action-app"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "all-action-app"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "app", Image: "nginx:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "all-action-pod-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "all-action-app"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:latest"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "app", Ready: true},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: deployName,
					},
					Actions:   []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionAll},
					TailLines: 50,
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
			deploy := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: "default"}, deploy); err == nil {
				Expect(k8sClient.Delete(ctx, deploy)).To(Succeed())
			}
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "all-action-pod-1", Namespace: "default"}, pod); err == nil {
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			}
			// Clean up potential ConfigMaps created by log/event collection
			for _, suffix := range []string{"-logs", "-events"} {
				cm := &corev1.ConfigMap{}
				cmNN := types.NamespacedName{Name: resourceName + suffix, Namespace: "default"}
				if err := k8sClient.Get(ctx, cmNN, cm); err == nil {
					_ = k8sClient.Delete(ctx, cm)
				}
			}
		})

		It("should attempt all actions including events collection", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				// Note: Clientset is nil so log collection will fail gracefully,
				// but events collection uses the controller-runtime client
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseCompleted))

			// Events ConfigMap should be created
			Expect(fetched.Status.EventsConfigMap).To(Equal(fmt.Sprintf("%s-events", resourceName)))
		})
	})

	Context("When testing the events action", func() {
		const resourceName = "events-action-tr"
		const deployName = "events-action-deploy"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			replicas := int32(1)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployName,
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "events-action-app"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "events-action-app"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "app", Image: "nginx:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "events-action-pod-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "events-action-app"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:latest"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "app", Ready: true},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: deployName,
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionEvents},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			for _, name := range []string{resourceName} {
				tr := &assistv1alpha1.TroubleshootRequest{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, tr); err == nil {
					_ = k8sClient.Delete(ctx, tr)
				}
			}
			deploy := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: "default"}, deploy); err == nil {
				_ = k8sClient.Delete(ctx, deploy)
			}
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "events-action-pod-1", Namespace: "default"}, pod); err == nil {
				_ = k8sClient.Delete(ctx, pod)
			}
			cm := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-events", Namespace: "default"}, cm); err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}
		})

		It("should create events ConfigMap", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseCompleted))
			Expect(fetched.Status.EventsConfigMap).To(Equal(fmt.Sprintf("%s-events", resourceName)))

			// Verify the ConfigMap exists
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fetched.Status.EventsConfigMap,
				Namespace: "default",
			}, cm)).To(Succeed())
			Expect(cm.Data).To(HaveKey("events"))
		})
	})

	Context("Helper function tests", func() {
		It("generateSummary should handle zero issues", func() {
			r := &TroubleshootRequestReconciler{}
			summary := r.generateSummary([]corev1.Pod{{}, {}}, nil)
			Expect(summary).To(ContainSubstring("2 pod(s) healthy"))
		})

		It("generateSummary should count severities correctly", func() {
			r := &TroubleshootRequestReconciler{}
			issues := []assistv1alpha1.DiagnosticIssue{
				{Severity: "Critical"},
				{Severity: "Critical"},
				{Severity: "Warning"},
			}
			summary := r.generateSummary([]corev1.Pod{{}}, issues)
			Expect(summary).To(ContainSubstring("2 critical"))
			Expect(summary).To(ContainSubstring("1 warning"))
		})

		It("generateSummary should handle warnings only", func() {
			r := &TroubleshootRequestReconciler{}
			issues := []assistv1alpha1.DiagnosticIssue{
				{Severity: "Warning"},
				{Severity: "Warning"},
			}
			summary := r.generateSummary([]corev1.Pod{{}}, issues)
			Expect(summary).To(ContainSubstring("2 warning"))
			Expect(summary).NotTo(ContainSubstring("critical"))
		})

		It("filterRecentRelevantEvents should include only recent target events", func() {
			now := time.Now()
			since := now.Add(-1 * time.Hour)
			targets := map[string]struct{}{
				"target-deploy": {},
				"pod-a":         {},
			}

			events := []corev1.Event{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "old",
						CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
					},
					InvolvedObject: corev1.ObjectReference{Name: "pod-a"},
					LastTimestamp:  metav1.NewTime(now.Add(-2 * time.Hour)),
					FirstTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
					Type:           "Warning",
					Reason:         "BackOff",
					Message:        "old event",
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "new-target",
						CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
					},
					InvolvedObject: corev1.ObjectReference{Name: "target-deploy"},
					LastTimestamp:  metav1.NewTime(now.Add(-5 * time.Minute)),
					Type:           "Normal",
					Reason:         "Pulled",
					Message:        "recent target event",
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "new-other",
						CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Minute)),
					},
					InvolvedObject: corev1.ObjectReference{Name: "unrelated"},
					LastTimestamp:  metav1.NewTime(now.Add(-2 * time.Minute)),
					Type:           "Normal",
					Reason:         "Pulled",
					Message:        "should not be included",
				},
			}

			lines := filterRecentRelevantEvents(events, targets, since)
			Expect(lines).To(HaveLen(1))
			Expect(lines[0]).To(ContainSubstring("recent target event"))
		})

		It("eventTimestamp should fall back across timestamp fields", func() {
			now := time.Now()

			event := corev1.Event{
				EventTime: metav1.MicroTime{Time: now.Add(-10 * time.Minute)},
			}
			Expect(eventTimestamp(event)).To(Equal(event.EventTime.Time))

			event = corev1.Event{
				FirstTimestamp: metav1.NewTime(now.Add(-15 * time.Minute)),
			}
			Expect(eventTimestamp(event)).To(Equal(event.FirstTimestamp.Time))
		})
	})

	Context("TTL cleanup for completed requests", func() {
		const resourceName = "ttl-completed-tr"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
		})

		It("should delete a completed request when TTL has expired", func() {
			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: "some-deploy",
					},
					TTLSecondsAfterFinished: ptr.To(int32(0)),
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			// Set status to Completed with a past CompletedAt
			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.PhaseCompleted
			pastTime := metav1.NewTime(time.Now().Add(-1 * time.Minute))
			fetched.Status.CompletedAt = &pastTime
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Resource should be deleted
			err = k8sClient.Get(ctx, nn, &assistv1alpha1.TroubleshootRequest{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected resource to be deleted by TTL")
		})

		It("should requeue a completed request when TTL has not expired", func() {
			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: "some-deploy",
					},
					TTLSecondsAfterFinished: ptr.To(int32(3600)),
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.PhaseCompleted
			now := metav1.Now()
			fetched.Status.CompletedAt = &now
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "should requeue for later TTL check")

			// Resource should still exist
			Expect(k8sClient.Get(ctx, nn, &assistv1alpha1.TroubleshootRequest{})).To(Succeed())
		})

		It("should not delete a completed request without TTL", func() {
			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: "some-deploy",
					},
					// No TTLSecondsAfterFinished
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.PhaseCompleted
			now := metav1.Now()
			fetched.Status.CompletedAt = &now
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// Resource should still exist
			Expect(k8sClient.Get(ctx, nn, &assistv1alpha1.TroubleshootRequest{})).To(Succeed())
		})
	})

	Context("TTL cleanup for failed requests", func() {
		const resourceName = "ttl-failed-tr"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			}
		})

		It("should delete a failed request when TTL has expired", func() {
			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: "some-deploy",
					},
					TTLSecondsAfterFinished: ptr.To(int32(0)),
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			// Set status to Failed with a past CompletedAt
			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.PhaseFailed
			pastTime := metav1.NewTime(time.Now().Add(-1 * time.Minute))
			fetched.Status.CompletedAt = &pastTime
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			// Resource should be deleted
			err = k8sClient.Get(ctx, nn, &assistv1alpha1.TroubleshootRequest{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected failed resource to be deleted by TTL")
		})

		It("should requeue a failed request when TTL has not expired", func() {
			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: "some-deploy",
					},
					TTLSecondsAfterFinished: ptr.To(int32(3600)),
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			fetched.Status.Phase = assistv1alpha1.PhaseFailed
			now := metav1.Now()
			fetched.Status.CompletedAt = &now
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "should requeue for later TTL check")

			// Resource should still exist
			Expect(k8sClient.Get(ctx, nn, &assistv1alpha1.TroubleshootRequest{})).To(Succeed())
		})
	})

	Context("When using the describe action", func() {
		const resourceName = "describe-action-tr"
		const deployName = "describe-action-deploy"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			replicas := int32(1)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployName,
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "describe-action-app"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "describe-action-app"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "app", Image: "nginx:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "describe-action-pod-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "describe-action-app"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:latest"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "app", Ready: true},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "Deployment",
						Name: deployName,
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDescribe},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				_ = k8sClient.Delete(ctx, tr)
			}
			deploy := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: "default"}, deploy); err == nil {
				_ = k8sClient.Delete(ctx, deploy)
			}
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "describe-action-pod-1", Namespace: "default"}, pod); err == nil {
				_ = k8sClient.Delete(ctx, pod)
			}
			cm := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-events", Namespace: "default"}, cm); err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}
		})

		It("should complete and create events ConfigMap for describe action", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				// Clientset nil — log collection will fail gracefully, but events uses controller-runtime client
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseCompleted))

			// Describe triggers events collection
			Expect(fetched.Status.EventsConfigMap).To(Equal(fmt.Sprintf("%s-events", resourceName)))

			// Verify the events ConfigMap exists
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fetched.Status.EventsConfigMap,
				Namespace: "default",
			}, cm)).To(Succeed())
			Expect(cm.Data).To(HaveKey("events"))
		})
	})

	Context("When targeting an unsupported kind", func() {
		It("should return an error from findTargetPods for unsupported target kind", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unsupported-kind-tr",
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "CronJob",
						Name: "some-cronjob",
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose},
				},
			}

			_, err := reconciler.findTargetPods(ctx, tr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported target kind"))
		})
	})

	Context("Default kind behavior", func() {
		const resourceName = "default-kind-tr"
		const deployName = "default-kind-deploy"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			replicas := int32(1)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployName,
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "default-kind-app"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "default-kind-app"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "app", Image: "nginx:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default-kind-pod-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "default-kind-app"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:latest"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "app", Ready: true},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			// Create TR with empty kind (defaults to Deployment)
			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{
						Kind: "", // Should default to Deployment
						Name: deployName,
					},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				_ = k8sClient.Delete(ctx, tr)
			}
			deploy := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: "default"}, deploy); err == nil {
				_ = k8sClient.Delete(ctx, deploy)
			}
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "default-kind-pod-1", Namespace: "default"}, pod); err == nil {
				_ = k8sClient.Delete(ctx, pod)
			}
		})

		It("should default to Deployment when kind is empty", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseCompleted))
		})
	})

	Context("setFailed helper", func() {
		const resourceName = "set-failed-tr"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				_ = k8sClient.Delete(ctx, tr)
			}
		})

		It("should set phase to Failed with summary and conditions", func() {
			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target: assistv1alpha1.TargetRef{Kind: "Deployment", Name: "x"},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
			Expect(k8sClient.Get(ctx, nn, tr)).To(Succeed())

			original := tr.DeepCopy()
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			err := reconciler.setFailed(ctx, original, tr, "test failure reason")
			Expect(err).NotTo(HaveOccurred())

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseFailed))
			Expect(fetched.Status.Summary).To(Equal("test failure reason"))
			Expect(fetched.Status.CompletedAt).NotTo(BeNil())
		})
	})

	Context("When targeting a ReplicaSet", func() {
		const resourceName = "rs-tr"
		const rsName = "test-rs"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			replicas := int32(1)
			rs := &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: "default",
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test-rs"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test-rs"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "app", Image: "nginx:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, rs)).To(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rs-pod-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "test-rs"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:latest"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			pod.Status = corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Name: "app", Ready: true}},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			tr := &assistv1alpha1.TroubleshootRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: assistv1alpha1.TroubleshootRequestSpec{
					Target:  assistv1alpha1.TargetRef{Kind: "ReplicaSet", Name: rsName},
					Actions: []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())
		})

		AfterEach(func() {
			tr := &assistv1alpha1.TroubleshootRequest{}
			if err := k8sClient.Get(ctx, nn, tr); err == nil {
				_ = k8sClient.Delete(ctx, tr)
			}
			rs := &appsv1.ReplicaSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: "default"}, rs); err == nil {
				_ = k8sClient.Delete(ctx, rs)
			}
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-rs-pod-1", Namespace: "default"}, pod); err == nil {
				_ = k8sClient.Delete(ctx, pod)
			}
		})

		It("should reconcile ReplicaSet targets", func() {
			reconciler := &TroubleshootRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			fetched := &assistv1alpha1.TroubleshootRequest{}
			Expect(k8sClient.Get(ctx, nn, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(assistv1alpha1.PhaseCompleted))
		})
	})

	Context("When diagnosing a pod with terminated container", func() {
		It("should detect terminated container issues", func() {
			reconciler := &TroubleshootRequestReconciler{}
			pods := []corev1.Pod{{
				ObjectMeta: metav1.ObjectMeta{Name: "term-pod", Namespace: "default"},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{{
						Name:  "app",
						Ready: false,
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								Reason:   "OOMKilled",
								ExitCode: 137,
							},
						},
					}},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "app",
						Image: "nginx:latest",
					}},
				},
			}}

			issues := reconciler.diagnosePodsDetailed(pods)
			Expect(issues).NotTo(BeEmpty())
			foundTerminated := false
			for _, issue := range issues {
				if issue.Type == "ContainerNotReady" && strings.Contains(issue.Message, "terminated") {
					foundTerminated = true
				}
			}
			Expect(foundTerminated).To(BeTrue(), "should detect terminated container")
		})
	})

	Context("When diagnosing a pod with scheduling failure", func() {
		It("should detect scheduling failure issues", func() {
			reconciler := &TroubleshootRequestReconciler{}
			pods := []corev1.Pod{{
				ObjectMeta: metav1.ObjectMeta{Name: "sched-pod", Namespace: "default"},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					Conditions: []corev1.PodCondition{{
						Type:    corev1.PodScheduled,
						Status:  corev1.ConditionFalse,
						Message: "0/3 nodes are available",
					}},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "nginx:latest"}},
				},
			}}

			issues := reconciler.diagnosePodsDetailed(pods)
			foundScheduling := false
			for _, issue := range issues {
				if issue.Type == "SchedulingFailed" {
					foundScheduling = true
				}
			}
			Expect(foundScheduling).To(BeTrue(), "should detect scheduling failure")
		})
	})

	Context("When diagnosing a pod with PodNotReady condition", func() {
		It("should detect PodNotReady condition", func() {
			reconciler := &TroubleshootRequestReconciler{}
			pods := []corev1.Pod{{
				ObjectMeta: metav1.ObjectMeta{Name: "ready-cond-pod", Namespace: "default"},
				Status: corev1.PodStatus{
					Phase:             corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{{Name: "app", Ready: true}},
					Conditions: []corev1.PodCondition{{
						Type:    corev1.PodReady,
						Status:  corev1.ConditionFalse,
						Reason:  "ReadinessGateFailed",
						Message: "readiness gate not met",
					}},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "nginx:latest"}},
				},
			}}

			issues := reconciler.diagnosePodsDetailed(pods)
			foundNotReady := false
			for _, issue := range issues {
				if issue.Type == "PodNotReady" {
					foundNotReady = true
				}
			}
			Expect(foundNotReady).To(BeTrue(), "should detect PodNotReady condition")
		})
	})

})
