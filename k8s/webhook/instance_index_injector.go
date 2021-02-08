package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"code.cloudfoundry.org/eirini"
	"code.cloudfoundry.org/eirini/k8s/stset"
	"code.cloudfoundry.org/eirini/util"
	eirinix "code.cloudfoundry.org/eirinix"
	"code.cloudfoundry.org/lager"
	exterrors "github.com/pkg/errors"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

//counterfeiter:generate -o webhookfakes/fake_manager.go code.cloudfoundry.org/eirinix.Manager

type Decoder interface {
	Decode(req admission.Request, into runtime.Object) error
}

type InstanceIndexEnvInjector struct {
	logger  lager.Logger
	decoder Decoder
}

func NewInstanceIndexEnvInjector(logger lager.Logger, decoder Decoder) InstanceIndexEnvInjector {
	return InstanceIndexEnvInjector{
		logger:  logger,
		decoder: decoder,
	}
}

func (i InstanceIndexEnvInjector) Handle(ctx context.Context, _ eirinix.Manager, _ *corev1.Pod, req admission.Request) admission.Response {
	logger := i.logger.Session("handle-webhook-request")

	if req.Operation != v1beta1.Create {
		return admission.Allowed("pod was already created")
	}

	pod := &corev1.Pod{}
	err := i.decoder.Decode(req, pod)
	if err != nil {
		logger.Error("no-pod-in-request", err)

		return admission.Errored(http.StatusBadRequest, err)
	}

	logger = logger.WithData(lager.Data{"pod-name": pod.Name, "pod-namespace": pod.Namespace})

	podCopy := pod.DeepCopy()

	err = injectInstanceIndex(logger, podCopy)
	if err != nil {
		i.logger.Error("failed-to-inject-instance-index", err)

		return admission.Errored(http.StatusBadRequest, err)
	}

	marshaledPod, err := json.Marshal(podCopy)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func injectInstanceIndex(logger lager.Logger, pod *corev1.Pod) error {
	index, err := util.ParseAppIndex(pod.Name)
	if err != nil {
		return exterrors.Wrap(err, "failed to parse app index")
	}

	for c := range pod.Spec.Containers {
		container := &pod.Spec.Containers[c]
		if container.Name == stset.OPIContainerName {
			cfInstanceVar := corev1.EnvVar{Name: eirini.EnvCFInstanceIndex, Value: strconv.Itoa(index)}
			container.Env = append(container.Env, cfInstanceVar)

			logger.Debug("patching-instance-index", lager.Data{"env-var": cfInstanceVar})

			return nil
		}
	}

	logger.Info("no-opi-container-found")

	return errors.New("no opi container found in pod")
}
