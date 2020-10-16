package task

import (
	"context"
	"time"

	"code.cloudfoundry.org/eirini/k8s"
	"code.cloudfoundry.org/lager"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//counterfeiter:generate . Reporter
//counterfeiter:generate . JobsClient
//counterfeiter:generate . Deleter

type Reporter interface {
	Report(*corev1.Pod) error
}

type JobsClient interface {
	GetByGUID(guid string) ([]batchv1.Job, error)
}

type Deleter interface {
	Delete(guid string) (string, error)
}

type Reconciler struct {
	logger             lager.Logger
	pods               client.Client
	jobs               JobsClient
	reporter           Reporter
	deleter            Deleter
	callbackRetryLimit int
	callbackRetries    map[string]int
	reported           map[string]bool
	ttlSeconds         int
}

func NewReconciler(
	logger lager.Logger,
	podClient client.Client,
	jobsClient JobsClient,
	reporter Reporter,
	deleter Deleter,
	callbackRetryLimit int,
	ttlSeconds int,
) *Reconciler {
	return &Reconciler{
		logger:             logger,
		pods:               podClient,
		jobs:               jobsClient,
		reporter:           reporter,
		deleter:            deleter,
		callbackRetryLimit: callbackRetryLimit,
		callbackRetries:    map[string]int{},
		reported:           map[string]bool{},
		ttlSeconds:         ttlSeconds,
	}
}

func (r Reconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	logger := r.logger.Session("task-completion-reconciler", lager.Data{"namespace": request.Namespace, "pod-name": request.Name})

	pod := &corev1.Pod{}
	if err := r.pods.Get(context.Background(), request.NamespacedName, pod); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Error("pod does not exist", err)

			return reconcile.Result{}, nil
		}

		logger.Error("failed to get pod", err)

		return reconcile.Result{}, err
	}

	if !r.taskContainerHasTerminated(logger, pod) {
		return reconcile.Result{}, nil
	}

	guid := pod.Labels[k8s.LabelGUID]
	logger = logger.WithData(lager.Data{"guid": guid})

	jobsForPods, err := r.jobs.GetByGUID(guid)
	if err != nil {
		logger.Error("failed to get related job by guid", err)

		return reconcile.Result{}, err
	}

	if len(jobsForPods) == 0 {
		logger.Debug("no jobs found for this pod")

		return reconcile.Result{}, nil
	}

	if err = r.reportIfRequired(pod); err != nil {
		logger.Error("completion-callback-failed", err, lager.Data{"tries": r.callbackRetries[guid]})

		return reconcile.Result{}, err
	}

	if !r.taskHasExpired(logger, pod) {
		logger.Debug("task-hasnt-expired-yet")

		return reconcile.Result{RequeueAfter: time.Duration(r.ttlSeconds) * time.Second}, nil
	}

	delete(r.callbackRetries, guid)
	_, err = r.deleter.Delete(guid)

	if err == nil {
		delete(r.reported, guid)
	}

	return reconcile.Result{}, err
}

func (r *Reconciler) reportIfRequired(pod *corev1.Pod) error {
	guid := pod.Labels[k8s.LabelGUID]

	if !r.reported[guid] && r.callbackRetries[guid] < r.callbackRetryLimit {
		if err := r.reporter.Report(pod); err != nil {
			r.callbackRetries[guid]++

			return err
		}

		r.reported[guid] = true
	}

	return nil
}

func (r Reconciler) taskContainerHasTerminated(logger lager.Logger, pod *corev1.Pod) bool {
	status, ok := getTaskContainerStatus(pod)
	if !ok {
		logger.Info("pod-has-no-task-container-status")

		return false
	}

	return status.State.Terminated != nil
}

func (r Reconciler) taskHasExpired(logger lager.Logger, pod *corev1.Pod) bool {
	status, ok := getTaskContainerStatus(pod)
	if !ok {
		logger.Info("pod-has-no-task-container-status")

		return false
	}

	ttlExpire := time.Now().Add(-time.Duration(r.ttlSeconds) * time.Second)

	return status.State.Terminated.FinishedAt.Time.Before(ttlExpire)
}
