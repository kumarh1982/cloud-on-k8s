// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package trial

import (
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/elastic/cloud-on-k8s/pkg/controller/common"
	licensing "github.com/elastic/cloud-on-k8s/pkg/controller/common/license"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/operator"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
	pkgerrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	name              = "trial-controller"
	EULAValidationMsg = `Please set the annotation elastic.co/eula to "accepted" to accept the EULA`
	trialOnlyOnceMsg  = "trial can be started only once"
)

var (
	log              = logf.Log.WithName(name)
	userFriendlyMsgs = map[licensing.LicenseStatus]string{
		licensing.LicenseStatusInvalid: "trial license signature invalid",
		licensing.LicenseStatusExpired: "trial license expired",
	}
)

// ReconcileTrials reconciles Enterprise trial licenses.
type ReconcileTrials struct {
	k8s.Client
	recorder record.EventRecorder
	// iteration is the number of times this controller has run its Reconcile method.
	iteration         int64
	trialState        licensing.TrialState
	operatorNamespace string
}

// Reconcile watches a trial status secret. If it finds a trial license it checks whether a trial has been started.
// If not it starts the trial period if the user has expressed intent to do so.
// If a trial is already running it validates the trial license.
func (r *ReconcileTrials) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// atomically update the iteration to support concurrent runs.
	currentIteration := atomic.AddInt64(&r.iteration, 1)
	iterationStartTime := time.Now()
	log.Info("Start reconcile iteration", "iteration", currentIteration, "namespace", request.Namespace, "secret_name", request.Name)
	defer func() {
		log.Info("End reconcile iteration", "iteration", currentIteration, "took", time.Since(iterationStartTime), "namespace", request.Namespace, "secret_name", request.Name)
	}()

	secret, license, err := licensing.TrialLicense(r, request.NamespacedName)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Trial license secret has been deleted by user, but trial had been started previously.")
		return reconcile.Result{}, nil
	}
	if err != nil {
		return reconcile.Result{}, pkgerrors.Wrap(err, "while fetching trial license")
	}

	validationMsg := validateEULA(secret)
	if validationMsg != "" {
		setValidationMsg(&secret, validationMsg)
		return reconcile.Result{}, licensing.UpdateEnterpriseLicense(r, secret, license)
	}

	// 1. reconcile trial status secret
	if err := r.reconcileTrialStatus(request.NamespacedName); err != nil {
		return reconcile.Result{}, err
	}

	// 2. reconcile the trial license itself
	trialSecretPopulated := license.IsMissingFields() == nil
	switch {
	case r.trialState.IsTrialRunning() && !trialSecretPopulated:
		// if the trial license fields are not populated at this point a user is trying to start a trial a second time
		// with an empty trial secret, which is not a supported use case.
		setValidationMsg(&secret, trialOnlyOnceMsg)
	case !trialSecretPopulated && r.trialState.IsTrialActivationInProgress():
		// trial is not running yet and the license secret is empty: init the trial
		if err := r.trialState.InitTrialLicense(&license); err != nil {
			return reconcile.Result{}, err
		}
	case trialSecretPopulated:
		status := r.trialState.LicenseVerifier().Valid(license, time.Now())
		if status != licensing.LicenseStatusValid {
			setValidationMsg(&secret, userFriendlyMsgs[status])
		} else {
			// valid trial license: complete trial activation
			return r.completeTrialActivation()
		}
	}
	return reconcile.Result{}, licensing.UpdateEnterpriseLicense(r, secret, license)
}

func (r *ReconcileTrials) reconcileTrialStatus(license types.NamespacedName) error {
	var trialStatus corev1.Secret
	var err error
	err = r.Get(types.NamespacedName{Namespace: r.operatorNamespace, Name: licensing.TrialStatusSecretKey}, &trialStatus)
	if errors.IsNotFound(err) {
		if !r.trialState.IsTrialRunning() {
			// we have no key in memory nor in the status: generate a new one
			if err := r.startTrialActivation(); err != nil {
				return err
			}
		}

		// we have the key in memory but the status secret is missing: recreate it
		trialStatus, err = licensing.ExpectedTrialStatus(r.operatorNamespace, license, r.trialState)
		if err != nil {
			return fmt.Errorf("while creating expected trial status %w", err)
		}
		return r.Create(&trialStatus)
	}
	if err != nil {
		return fmt.Errorf("while fetching trial status %w", err)
	}

	// the status is there but we don't have anything in memory recover the keys
	if r.trialState.IsEmpty() {
		recoveredKeys, err := licensing.NewTrialStateFromStatus(trialStatus)
		if err != nil {
			return err
		}
		r.trialState = recoveredKeys
	}
	// if trial status exists, but:
	// - has been tampered with: reconstruct it
	// - we need to update it to complete the trial activation
	// - we need to update it because we just regenerated it after a crash
	expected, err := licensing.ExpectedTrialStatus(r.operatorNamespace, license, r.trialState)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(expected.Data, trialStatus.Data) {
		return nil
	}
	trialStatus.Data = expected.Data
	return r.Update(&trialStatus)
}

func (r *ReconcileTrials) startTrialActivation() error {
	keys, err := licensing.NewTrialState()
	if err != nil {
		return err
	}
	r.trialState = keys
	return nil
}

func (r *ReconcileTrials) completeTrialActivation() (reconcile.Result, error) {
	if r.trialState.CompleteTrialActivation() {
		// requeue to update trial status
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{}, nil

}

func validateEULA(trialSecret corev1.Secret) string {
	if licensing.IsEnterpriseTrial(trialSecret) &&
		trialSecret.Annotations[licensing.EULAAnnotation] != licensing.EULAAcceptedValue {
		return EULAValidationMsg
	}
	return ""
}

func setValidationMsg(secret *corev1.Secret, violation string) {
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	log.Info("trial license invalid", "reason", violation)
	secret.Annotations[licensing.LicenseInvalidAnnotation] = violation
}

func newReconciler(mgr manager.Manager, params operator.Parameters) *ReconcileTrials {
	return &ReconcileTrials{
		Client:            k8s.WrapClient(mgr.GetClient()),
		recorder:          mgr.GetEventRecorderFor(name),
		operatorNamespace: params.OperatorNamespace,
	}
}

func addWatches(c controller.Controller) error {

	// Watch the trial status secret and the enterprise trial licenses as well
	if err := c.Watch(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestsFromMapFunc{
		ToRequests: handler.ToRequestsFunc(func(obj handler.MapObject) []reconcile.Request {
			secret, ok := obj.Object.(*corev1.Secret)
			if !ok {
				log.Error(fmt.Errorf("object of type %T in secret watch", obj.Object), "dropping event due to type error")
			}
			if licensing.IsEnterpriseTrial(*secret) {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Namespace: obj.Meta.GetNamespace(),
							Name:      obj.Meta.GetName(),
						},
					},
				}
			}

			if obj.Meta.GetName() != licensing.TrialStatusSecretKey {
				return nil
			}
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: secret.Annotations[licensing.TrialLicenseSecretNamespace],
						Name:      secret.Annotations[licensing.TrialLicenseSecretName],
					},
				},
			}
		}),
	}); err != nil {
		return err
	}
	return nil
}

// Add creates a new Trial Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, params operator.Parameters) error {
	r := newReconciler(mgr, params)
	c, err := common.NewController(mgr, name, r, params)
	if err != nil {
		return err
	}
	return addWatches(c)
}

var _ reconcile.Reconciler = &ReconcileTrials{}
