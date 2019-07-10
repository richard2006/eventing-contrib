/*
Copyright 2019 The Knative Authors.

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

package alimns

import (
	"context"

	"fmt"
	"log"
	"os"

	"github.com/knative/eventing-contrib/contrib/alimns/pkg/apis/sources/v1alpha1"
	"github.com/knative/eventing-contrib/contrib/alimns/pkg/reconciler/resources"
	"github.com/knative/eventing-contrib/pkg/controller/sdk"
	"github.com/knative/eventing-contrib/pkg/controller/sinks"
	"github.com/knative/eventing-contrib/pkg/reconciler/eventtype"
	eventingv1alpha1 "github.com/knative/eventing/pkg/apis/eventing/v1alpha1"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/pkg/logging"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	// controllerAgentName is the string used by this controller to identify
	// itself when creating events.
	controllerAgentName = "alimns-source-controller"

	// raImageEnvVar is the name of the environment variable that contains the receive adapter's
	// image. It must be defined.
	raImageEnvVar = "MNS_RA_IMAGE"

	finalizerName = controllerAgentName
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new AliMnsSource Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, logger *zap.SugaredLogger) error {
	raImage, defined := os.LookupEnv(raImageEnvVar)
	if !defined {
		return fmt.Errorf("required environment variable '%s' not defined", raImageEnvVar)
	}
	log.Println("Adding the MNS Source controller.")
	p := &sdk.Provider{
		AgentName: controllerAgentName,
		Parent:    &v1alpha1.AliMnsSource{},
		Owns:      []runtime.Object{&appsv1.Deployment{}, &eventingv1alpha1.EventType{}},
		Reconciler: &reconciler{
			scheme:              mgr.GetScheme(),
			mnsClientCreator:    AliMNSClientCreator,
			receiveAdapterImage: raImage,
			eventTypeReconciler: eventtype.Reconciler{
				Scheme: mgr.GetScheme(),
			},
		},
	}

	return p.Add(mgr, logger)
}

type reconciler struct {
	client              client.Client
	scheme              *runtime.Scheme
	mnsClientCreator    MNSClientCreator
	receiveAdapterImage string
	eventTypeReconciler eventtype.Reconciler
}

func (r *reconciler) InjectClient(c client.Client) error {
	r.client = c
	r.eventTypeReconciler.Client = c
	return nil
}

func (r *reconciler) Reconcile(ctx context.Context, object runtime.Object) error {
	logger := logging.FromContext(ctx).Desugar()

	src, ok := object.(*v1alpha1.AliMnsSource)
	if !ok {
		logger.Error("could not find MNS source", zap.Any("object", object))
		return nil
	}

	// This Source attempts to reconcile three things.
	// 1. Determine the sink's URI.
	//     - Nothing to delete.
	// 2. Create a receive adapter in the form of a Deployment.
	//     - Will be garbage collected by K8s when this AliMnsSource is deleted.
	// 3. Register that receive adapter as a Pull endpoint for the specified MNS Topic.
	//     - This needs to deregister during deletion.
	// 4. Create the EventTypes that it can emit.
	//     - Will be garbage collected by K8s when this AliMnsSource is deleted.
	// Because there is something that must happen during deletion, we add this controller as a
	// finalizer to every AliMnsSource.

	// See if the source has been deleted.
	deletionTimestamp := src.DeletionTimestamp
	if deletionTimestamp != nil {
		err := r.deleteSubscription(ctx, src)
		if err != nil {
			logger.Error("Unable to delete the Subscription", zap.Error(err))
			return err
		}
		r.removeFinalizer(src)
		return nil
	}

	r.addFinalizer(src)

	src.Status.InitializeConditions()

	sinkURI, err := sinks.GetSinkURI(ctx, r.client, src.Spec.Sink, src.Namespace)
	if err != nil {
		src.Status.MarkNoSink("NotFound", "")
		return err
	}
	src.Status.MarkSink(sinkURI)

	var transformerURI string
	if src.Spec.Transformer != nil {
		transformerURI, err = sinks.GetSinkURI(ctx, r.client, src.Spec.Transformer, src.Namespace)
		if err != nil {
			src.Status.MarkNoTransformer("NotFound", "")
			return err
		}
		src.Status.MarkTransformer(transformerURI)
	}

	sub, err := r.createSubscription(ctx, src)
	if err != nil {
		logger.Error("Unable to create the subscription", zap.Error(err))
		return err
	}
	src.Status.MarkSubscribed()

	_, err = r.createReceiveAdapter(ctx, src, sub, sinkURI, transformerURI)
	if err != nil {
		logger.Error("Unable to create the receive adapter", zap.Error(err))
		return err
	}
	src.Status.MarkDeployed()

	err = r.reconcileEventTypes(ctx, src)
	if err != nil {
		logger.Error("Unable to reconcile the event types", zap.Error(err))
		return err
	}
	src.Status.MarkEventTypes()

	return nil
}

func (r *reconciler) addFinalizer(s *v1alpha1.AliMnsSource) {
	finalizers := sets.NewString(s.Finalizers...)
	finalizers.Insert(finalizerName)
	s.Finalizers = finalizers.List()
}

func (r *reconciler) removeFinalizer(s *v1alpha1.AliMnsSource) {
	finalizers := sets.NewString(s.Finalizers...)
	finalizers.Delete(finalizerName)
	s.Finalizers = finalizers.List()
}

func (r *reconciler) createReceiveAdapter(ctx context.Context, src *v1alpha1.AliMnsSource, subscriptionID, sinkURI, transformerURI string) (*appsv1.Deployment, error) {
	ra, err := r.getReceiveAdapter(ctx, src)
	if err != nil && !apierrors.IsNotFound(err) {
		logging.FromContext(ctx).Error("Unable to get an existing receive adapter", zap.Error(err))
		return nil, err
	}
	if ra != nil {
		logging.FromContext(ctx).Desugar().Info("Reusing existing receive adapter", zap.Any("receiveAdapter", ra))
		return ra, nil
	}
	svc := resources.MakeReceiveAdapter(&resources.ReceiveAdapterArgs{
		Image:          r.receiveAdapterImage,
		Source:         src,
		Labels:         getLabels(src),
		SubscriptionID: subscriptionID,
		SinkURI:        sinkURI,
		TransformerURI: transformerURI,
	})
	if err := controllerutil.SetControllerReference(src, svc, r.scheme); err != nil {
		return nil, err
	}
	err = r.client.Create(ctx, svc)
	logging.FromContext(ctx).Desugar().Info("Receive Adapter created.", zap.Error(err), zap.Any("receiveAdapter", svc))
	return svc, err
}

func (r *reconciler) getReceiveAdapter(ctx context.Context, src *v1alpha1.AliMnsSource) (*appsv1.Deployment, error) {
	dl := &appsv1.DeploymentList{}
	err := r.client.List(ctx, &client.ListOptions{
		Namespace:     src.Namespace,
		LabelSelector: r.getLabelSelector(src),
		// TODO this is only needed by the fake client. Real K8s does not need it. Remove it once
		// the fake is fixed.
		Raw: &metav1.ListOptions{
			TypeMeta: metav1.TypeMeta{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
			},
		},
	},
		dl)

	if err != nil {
		logging.FromContext(ctx).Desugar().Error("Unable to list deployments: %v", zap.Error(err))
		return nil, err
	}
	for _, dep := range dl.Items {
		if metav1.IsControlledBy(&dep, src) {
			return &dep, nil
		}
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{}, "")
}

func (r *reconciler) getLabelSelector(src *v1alpha1.AliMnsSource) labels.Selector {
	return labels.SelectorFromSet(getLabels(src))
}

func getLabels(src *v1alpha1.AliMnsSource) map[string]string {
	return map[string]string{
		"knative-eventing-source":      controllerAgentName,
		"knative-eventing-source-name": src.Name,
	}
}

func (r *reconciler) createSubscription(ctx context.Context, src *v1alpha1.AliMnsSource) (string, error) {
	creds, err := GetCredentials(ctx, r.client, src)
	if err != nil {
		logging.FromContext(ctx).Desugar().Info("Failed to extract creds", zap.Error(err))
		return "", err
	}
	psc, err := r.mnsClientCreator(ctx, creds)
	if err != nil {
		return "", err
	}
	subName := generateSubName(src)
	err = psc.CreateSubscription(ctx, src.Spec.Topic, subName)
	if err != nil {
		logging.FromContext(ctx).Desugar().Info("Error creating new subscription", zap.Error(err))
	} else {
		logging.FromContext(ctx).Desugar().Info("Created new subscription", zap.Any("subscription", generateSubName(src)))
	}
	return subName, err
}

func (r *reconciler) deleteSubscription(ctx context.Context, src *v1alpha1.AliMnsSource) error {
	creds, err := GetCredentials(ctx, r.client, src)
	if err != nil {
		logging.FromContext(ctx).Desugar().Info("Failed to extract creds", zap.Error(err))
		return err
	}
	psc, err := r.mnsClientCreator(ctx, creds)
	if err != nil {
		return err
	}
	if exists, err := psc.SubscriptionExists(ctx, src.Spec.Topic, generateSubName(src)); err != nil {
		return err
	} else if !exists {
		return nil
	}
	return psc.DeleteSubscription(ctx, src.Spec.Topic, generateSubName(src))
}

func (r *reconciler) reconcileEventTypes(ctx context.Context, src *v1alpha1.AliMnsSource) error {
	args := r.newEventTypeReconcilerArgs(src)
	return r.eventTypeReconciler.Reconcile(ctx, src, args)
}

func (r *reconciler) newEventTypeReconcilerArgs(src *v1alpha1.AliMnsSource) *eventtype.ReconcilerArgs {
	spec := eventingv1alpha1.EventTypeSpec{
		Type:   v1alpha1.AliMnsSourceEventType,
		Source: v1alpha1.AliMnsEventSource(src.Spec.Topic),
		Broker: src.Spec.Sink.Name,
	}
	specs := make([]eventingv1alpha1.EventTypeSpec, 0, 1)
	specs = append(specs, spec)
	return &eventtype.ReconcilerArgs{
		Specs:     specs,
		Namespace: src.Namespace,
		Labels:    getLabels(src),
		Kind:      src.Spec.Sink.Kind,
	}
}

func generateSubName(src *v1alpha1.AliMnsSource) string {
	return fmt.Sprintf("knative-eventing-%s-%s-%s", src.Namespace, src.Name, src.UID)
}
