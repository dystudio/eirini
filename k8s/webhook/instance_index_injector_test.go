package webhook_test

import (
	"context"
	"encoding/json"
	"net/http"

	"code.cloudfoundry.org/eirini/k8s/webhook"
	eirinix "code.cloudfoundry.org/eirinix"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"gomodules.xyz/jsonpatch/v2"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ = Describe("InstanceIndexInjector", func() {
	var (
		injector eirinix.Extension
		logger   lager.Logger
		pod      *corev1.Pod
		req      admission.Request
		resp     admission.Response
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("instance-index-injector")
		decoder, err := admission.NewDecoder(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())
		injector = webhook.NewInstanceIndexEnvInjector(logger, decoder)

		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-app-instance-3",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "opi",
						Env: []corev1.EnvVar{
							{Name: "FOO", Value: "foo"},
							{Name: "BAR", Value: "bar"},
						},
					},
				},
			},
		}

		rawPod, err := json.Marshal(pod)
		Expect(err).NotTo(HaveOccurred())

		req = admission.Request{
			AdmissionRequest: v1beta1.AdmissionRequest{
				Operation: v1beta1.Create,
				Object:    runtime.RawExtension{Raw: rawPod},
			},
		}
	})

	JustBeforeEach(func() {
		resp = injector.Handle(context.Background(), nil, nil, req)
	})

	It("injects the app instance as env variable in the container", func() {
		Expect(resp.Patches).To(Equal(
			[]jsonpatch.Operation{
				{
					Operation: "add",
					Path:      "/spec/containers/0/env/2",
					Value: map[string]interface{}{
						"name":  "CF_INSTANCE_INDEX",
						"value": "3",
					},
				},
			},
		))
	})

	Context("the passed pod has already been created", func() {
		When("operation is Update", func() {
			BeforeEach(func() {
				req.AdmissionRequest.Operation = v1beta1.Update
			})

			It("allows the operation without interacting with the passed pod", func() {
				ExpectAllowResponse(resp)
			})
		})

		When("operation is Delete", func() {
			BeforeEach(func() {
				req.AdmissionRequest.Operation = v1beta1.Delete
			})

			It("allows the operation without interacting with the passed pod", func() {
				ExpectAllowResponse(resp)
			})
		})

		When("operation is Connect", func() {
			BeforeEach(func() {
				req.AdmissionRequest.Operation = v1beta1.Connect
			})

			It("allows the operation without interacting with the passed pod", func() {
				ExpectAllowResponse(resp)
			})
		})
	})

	When("the pod name has no dashes", func() {
		BeforeEach(func() {
			pod.Name = "myinstance4"
		})

		It("returns an error response", func() {
			ExpectBadRequestErrorResponse(resp, "could not parse app name")
		})
	})

	When("the pod name is empty", func() {
		BeforeEach(func() {
			pod.Name = ""
		})

		It("returns an error response", func() {
			ExpectBadRequestErrorResponse(resp, "could not parse app name")
		})
	})

	When("pod name part after final dash is not numeric", func() {
		BeforeEach(func() {
			pod.Name = "my-instance-four"
		})

		It("returns an error response", func() {
			ExpectBadRequestErrorResponse(resp, "pod my-instance-four name does not contain an index")
		})
	})

	When("pod name ends with a dash", func() {
		BeforeEach(func() {
			pod.Name = "my-instance-"
		})

		It("returns an error response", func() {
			ExpectBadRequestErrorResponse(resp, "pod my-instance- name does not contain an index")
		})
	})

	When("the pod has no OPI container", func() {
		BeforeEach(func() {
			pod.Spec.Containers[0].Name = "ipo"
		})

		It("returns an error response", func() {
			ExpectBadRequestErrorResponse(resp, "no opi container found in pod")
		})
	})
})

func ExpectBadRequestErrorResponse(resp admission.Response, msg string) {
	ExpectWithOffset(1, resp.Allowed).To(BeFalse())
	ExpectWithOffset(1, resp.Result).ToNot(BeNil())
	ExpectWithOffset(1, resp.Result.Code).To(Equal(int32(http.StatusBadRequest)))
	ExpectWithOffset(1, resp.Result.Message).To(ContainSubstring(msg))
	ExpectWithOffset(1, resp.Patches).To(BeEmpty())
}

func ExpectAllowResponse(resp admission.Response) {
	ExpectWithOffset(1, resp.Allowed).To(BeTrue())
	ExpectWithOffset(1, resp.Result.Code).To(Equal(int32(http.StatusOK)))
	ExpectWithOffset(1, resp.Result.Reason).To(Equal(metav1.StatusReason("pod was already created")))
	ExpectWithOffset(1, resp.Patches).To(BeEmpty())
}
