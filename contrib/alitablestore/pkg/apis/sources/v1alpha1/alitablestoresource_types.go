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

package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis/duck"
	duckv1alpha1 "knative.dev/pkg/apis/duck/v1alpha1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Check that AliTablestoreSource can be validated and can be defaulted.
var _ runtime.Object = (*AliTablestoreSource)(nil)

// Check that AliTablestoreSource implements the Conditions duck type.
var _ = duck.VerifyType(&AliTablestoreSource{}, &duckv1alpha1.Conditions{})

const (
	// TablestoreSourceEventType is the tablestore CloudEvent type, in case tablestore doesn't send a
	// CloudEvent itself.
	AliTablestoreSourceEventType = "alicloud.tablestore"
)

//// AliTablestoreEventType returns the AliTablestore CloudEvent type value.
//func AliTablestoreEventType(ghEventType string) string {
//	return fmt.Sprintf("%s.%s", AliTablestoreSourceEventTypePrefix, ghEventType)
//}

// AliTablestoreEventSource returns the tablestore CloudEvent source value.
func AliTablestoreEventSource(topic string) string {
	return fmt.Sprintf("%s", topic)
}

// AliTablestoreSourceSpec defines the desired state of AliTablestoreSource
type AliTablestoreSourceSpec struct {
	// AccessToken is the Kubernetes secret containing the tablestore
	// access token
	AccessToken SecretValueFromSource `json:"accessToken"`
	// Topic is the ID of the GCP PubSub Topic to Subscribe to. It must be in the form of the
	// unique identifier within the project, not the entire name. E.g. it must be 'laconia', not
	// 'projects/my-gcp-project/topics/laconia'.
	TableName string `json:"tableName,omitempty"`

	Instance string `json:"instance,omitempty"`
	// Sink is a reference to an object that will resolve to a domain name to use as the sink.
	// +optional
	Sink *corev1.ObjectReference `json:"sink,omitempty"`

	// Transformer is a reference to an object that will resolve to a domain name to use as the transformer.
	// +optional
	Transformer *corev1.ObjectReference `json:"transformer,omitempty"`

	// ServiceAccoutName is the name of the ServiceAccount that will be used to run the Receive
	// Adapter Deployment.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
}

// SecretValueFromSource represents the source of a secret value
type SecretValueFromSource struct {
	// The Secret key to select from.
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
}

type Credentials struct {
	Url             string `json:"url"`
	AccessKeyId     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
}

// AliTablestoreSourceStatus defines the observed state of AliTablestoreSource
type AliTablestoreSourceStatus struct {
	// inherits duck/v1alpha1 Status, which currently provides:
	// * ObservedGeneration - the 'Generation' of the Service that was last processed by the controller.
	// * Conditions - the latest available observations of a resource's current state.
	duckv1alpha1.Status `json:",inline"`

	// SinkURI is the current active sink URI that has been configured for the AliTablestoreSource.
	// +optional
	SinkURI string `json:"sinkUri,omitempty"`

	// TransformerURI is the current active transformer URI that has been configured for the AliTablestoreSource.
	// +optional
	TransformerURI string `json:"transformerUri,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AliTablestoreSource is the Schema for the AliTablestoreSource API
// +k8s:openapi-gen=true
type AliTablestoreSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AliTablestoreSourceSpec   `json:"spec,omitempty"`
	Status AliTablestoreSourceStatus `json:"status,omitempty"`
}

const (
	// TablestoreConditionReady has status True when the TablestoreSource is ready to send events.
	TablestoreConditionReady = duckv1alpha1.ConditionReady

	// TablestoreConditionSinkProvided has status True when the TablestoreSource has been configured with a sink target.
	TablestoreConditionSinkProvided duckv1alpha1.ConditionType = "SinkProvided"

	// TablestoreConditionTransformerProvided has status True when the TablestoreSource has been configured with a transformer target.
	TablestoreConditionTransformerProvided duckv1alpha1.ConditionType = "TransformerProvided"

	// TablestoreConditionDeployed has status True when the TablestoreSource has had it's receive adapter deployment created.
	TablestoreConditionDeployed duckv1alpha1.ConditionType = "Deployed"

	// TablestoreConditionSubscribed has status True when a Tablestore Subscription has been created pointing at the created receive adapter deployment.
	TablestoreConditionSubscribed duckv1alpha1.ConditionType = "Subscribed"

	// TablestoreConditionEventTypesProvided has status True when the TablestoreSource has been configured with event types.
	TablestoreConditionEventTypesProvided duckv1alpha1.ConditionType = "EventTypesProvided"
)

var tablestoreSourceCondSet = duckv1alpha1.NewLivingConditionSet(
	TablestoreConditionSinkProvided,
	TablestoreConditionDeployed,
	TablestoreConditionSubscribed)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GetCondition returns the condition currently associated with the given type, or nil.
func (s *AliTablestoreSourceStatus) GetCondition(t duckv1alpha1.ConditionType) *duckv1alpha1.Condition {
	return tablestoreSourceCondSet.Manage(s).GetCondition(t)
}

// IsReady returns true if the resource is ready overall.
func (s *AliTablestoreSourceStatus) IsReady() bool {
	return tablestoreSourceCondSet.Manage(s).IsHappy()
}

// InitializeConditions sets relevant unset conditions to Unknown state.
func (s *AliTablestoreSourceStatus) InitializeConditions() {
	tablestoreSourceCondSet.Manage(s).InitializeConditions()
}

// MarkSink sets the condition that the source has a sink configured.
func (s *AliTablestoreSourceStatus) MarkSink(uri string) {
	s.SinkURI = uri
	if len(uri) > 0 {
		tablestoreSourceCondSet.Manage(s).MarkTrue(TablestoreConditionSinkProvided)
	} else {
		tablestoreSourceCondSet.Manage(s).MarkUnknown(TablestoreConditionSinkProvided, "SinkEmpty", "Sink has resolved to empty.")
	}
}

// MarkSink sets the condition that the source has a transformer configured.
func (s *AliTablestoreSourceStatus) MarkTransformer(uri string) {
	s.TransformerURI = uri
	if len(uri) > 0 {
		tablestoreSourceCondSet.Manage(s).MarkTrue(TablestoreConditionTransformerProvided)
	} else {
		tablestoreSourceCondSet.Manage(s).MarkUnknown(TablestoreConditionTransformerProvided, "TransformerEmpty", "Transformer has resolved to empty.")
	}
}

// MarkNoSink sets the condition that the source does not have a sink configured.
func (s *AliTablestoreSourceStatus) MarkNoSink(reason, messageFormat string, messageA ...interface{}) {
	tablestoreSourceCondSet.Manage(s).MarkFalse(TablestoreConditionSinkProvided, reason, messageFormat, messageA...)
}

// MarkNoTransformer sets the condition that the source does not have a transformer configured.
func (s *AliTablestoreSourceStatus) MarkNoTransformer(reason, messageFormat string, messageA ...interface{}) {
	tablestoreSourceCondSet.Manage(s).MarkFalse(TablestoreConditionTransformerProvided, reason, messageFormat, messageA...)
}

// MarkDeployed sets the condition that the source has been deployed.
func (s *AliTablestoreSourceStatus) MarkDeployed() {
	tablestoreSourceCondSet.Manage(s).MarkTrue(TablestoreConditionDeployed)
}

// MarkDeploying sets the condition that the source is deploying.
func (s *AliTablestoreSourceStatus) MarkDeploying(reason, messageFormat string, messageA ...interface{}) {
	tablestoreSourceCondSet.Manage(s).MarkUnknown(TablestoreConditionDeployed, reason, messageFormat, messageA...)
}

// MarkNotDeployed sets the condition that the source has not been deployed.
func (s *AliTablestoreSourceStatus) MarkNotDeployed(reason, messageFormat string, messageA ...interface{}) {
	tablestoreSourceCondSet.Manage(s).MarkFalse(TablestoreConditionDeployed, reason, messageFormat, messageA...)
}

func (s *AliTablestoreSourceStatus) MarkSubscribed() {
	tablestoreSourceCondSet.Manage(s).MarkTrue(TablestoreConditionSubscribed)
}

// MarkEventTypes sets the condition that the source has created its event types.
func (s *AliTablestoreSourceStatus) MarkEventTypes() {
	tablestoreSourceCondSet.Manage(s).MarkTrue(TablestoreConditionEventTypesProvided)
}

// MarkNoEventTypes sets the condition that the source does not its event types configured.
func (s *AliTablestoreSourceStatus) MarkNoEventTypes(reason, messageFormat string, messageA ...interface{}) {
	tablestoreSourceCondSet.Manage(s).MarkFalse(TablestoreConditionEventTypesProvided, reason, messageFormat, messageA...)
}

// AliTablestoreSourceList contains a list of AliTablestoreSource
type AliTablestoreSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AliTablestoreSource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AliTablestoreSource{}, &AliTablestoreSourceList{})
}
