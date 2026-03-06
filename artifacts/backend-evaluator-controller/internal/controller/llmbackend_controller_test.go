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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	edgecloudlabsv1alpha1 "github.com/felipevicens/backend-evaluation-operator/api/v1alpha1"
)

var _ = Describe("LLMBackend Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		llmbackend := &edgecloudlabsv1alpha1.LLMBackend{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind LLMBackend")
			err := k8sClient.Get(ctx, typeNamespacedName, llmbackend)
			if err != nil && errors.IsNotFound(err) {
				resource := &edgecloudlabsv1alpha1.LLMBackend{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: edgecloudlabsv1alpha1.LLMBackendSpec{
						Deployment: edgecloudlabsv1alpha1.DeploymentLocal,
						Model:      "gpt-test",
						Endpoint:   "http://test-endpoint",
						APIKeySecretRef: &edgecloudlabsv1alpha1.SecretKeySelector{
							Name: "test-secret",
							Key:  "apiKey",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &edgecloudlabsv1alpha1.LLMBackend{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance LLMBackend")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &LLMBackendReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Recorder:  record.NewFakeRecorder(10),
				Clientset: fake.NewSimpleClientset(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify transition to BackendCreated phase
			updatedEval := &edgecloudlabsv1alpha1.LLMBackend{}
			err = k8sClient.Get(ctx, typeNamespacedName, updatedEval)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedEval.Status.Phase).To(Equal(edgecloudlabsv1alpha1.PhaseBackendCreated))
			Expect(updatedEval.Status.BackendName).To(Equal("gpt-test"))

			// Verify AgentGatewayBackend spec
			backend := &unstructured.Unstructured{}
			backend.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "agentgateway.dev",
				Version: "v1alpha1",
				Kind:    "AgentgatewayBackend",
			})
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "gpt-test", Namespace: "default"}, backend)
			Expect(err).NotTo(HaveOccurred())

			spec := backend.Object["spec"].(map[string]interface{})
			ai := spec["ai"].(map[string]interface{})
			provider := ai["provider"].(map[string]interface{})
			Expect(provider["host"]).To(Equal("test-endpoint"))
			Expect(provider["port"]).To(BeEquivalentTo(80))
			Expect(provider["path"]).To(Equal("/v1/chat/completions"))
			openai := provider["openai"].(map[string]interface{})
			Expect(openai["model"]).To(Equal("gpt-test"))

			// Verify HTTPRoute spec
			route := &unstructured.Unstructured{}
			route.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "gateway.networking.k8s.io",
				Version: "v1",
				Kind:    "HTTPRoute",
			})
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "gpt-test-route", Namespace: "default"}, route)
			Expect(err).NotTo(HaveOccurred())

			routeSpec := route.Object["spec"].(map[string]interface{})
			parentRefs := routeSpec["parentRefs"].([]interface{})
			Expect(parentRefs).To(HaveLen(1))
			parentRef := parentRefs[0].(map[string]interface{})
			Expect(parentRef["name"]).To(Equal("agentgateway-proxy"))
			Expect(parentRef["namespace"]).To(Equal("agentgateway-system"))

			rules := routeSpec["rules"].([]interface{})
			Expect(rules).To(HaveLen(1))
			rule := rules[0].(map[string]interface{})

			matches := rule["matches"].([]interface{})
			Expect(matches).To(HaveLen(1))
			match := matches[0].(map[string]interface{})
			headers := match["headers"].([]interface{})
			Expect(headers).To(HaveLen(1))
			header := headers[0].(map[string]interface{})
			Expect(header["name"]).To(Equal("x-vsr-selected-model"))
			Expect(header["value"]).To(Equal("gpt-test"))

			backendRefs := rule["backendRefs"].([]interface{})
			Expect(backendRefs).To(HaveLen(1))
			backendRef := backendRefs[0].(map[string]interface{})
			Expect(backendRef["name"]).To(Equal("gpt-test"))
			Expect(backendRef["namespace"]).To(Equal("default"))
		})
		It("should successfully reconcile a resource without an endpoint (public provider)", func() {
			resourceNamePublic := "public-resource"
			typeNamespacedNamePublic := types.NamespacedName{
				Name:      resourceNamePublic,
				Namespace: "default",
			}

			By("creating the custom resource without an endpoint")
			resource := &edgecloudlabsv1alpha1.LLMBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceNamePublic,
					Namespace: "default",
				},
				Spec: edgecloudlabsv1alpha1.LLMBackendSpec{
					Deployment: edgecloudlabsv1alpha1.DeploymentRemote,
					Model:      "gpt-public",
					APIKeySecretRef: &edgecloudlabsv1alpha1.SecretKeySelector{
						Name: "public-secret",
						Key:  "Authorization",
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &LLMBackendReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Recorder:  record.NewFakeRecorder(10),
				Clientset: fake.NewSimpleClientset(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedNamePublic,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify transition to BackendCreated phase
			updatedEval := &edgecloudlabsv1alpha1.LLMBackend{}
			err = k8sClient.Get(ctx, typeNamespacedNamePublic, updatedEval)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedEval.Status.Phase).To(Equal(edgecloudlabsv1alpha1.PhaseBackendCreated))

			// Verify AgentGatewayBackend spec (no host/port/path)
			backend := &unstructured.Unstructured{}
			backend.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "agentgateway.dev",
				Version: "v1alpha1",
				Kind:    "AgentgatewayBackend",
			})
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "gpt-public", Namespace: "default"}, backend)
			Expect(err).NotTo(HaveOccurred())

			spec := backend.Object["spec"].(map[string]interface{})
			ai := spec["ai"].(map[string]interface{})
			provider := ai["provider"].(map[string]interface{})

			Expect(provider).NotTo(HaveKey("host"))
			Expect(provider).NotTo(HaveKey("port"))
			Expect(provider).NotTo(HaveKey("path"))

			openai := provider["openai"].(map[string]interface{})
			Expect(openai["model"]).To(Equal("gpt-public"))

			// Verify HTTPRoute spec for public provider
			route := &unstructured.Unstructured{}
			route.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "gateway.networking.k8s.io",
				Version: "v1",
				Kind:    "HTTPRoute",
			})
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "gpt-public-route", Namespace: "default"}, route)
			Expect(err).NotTo(HaveOccurred())

			routeSpec := route.Object["spec"].(map[string]interface{})
			parentRefs := routeSpec["parentRefs"].([]interface{})
			Expect(parentRefs[0].(map[string]interface{})["name"]).To(Equal("agentgateway-proxy"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})
})
