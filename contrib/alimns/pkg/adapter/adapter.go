/*
Copyright 2018 The Knative Authors

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

package mns

import (
	"fmt"

	"time"

	cloudevents "github.com/cloudevents/sdk-go"
	"github.com/knative/eventing-contrib/pkg/kncloudevents"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"knative.dev/pkg/logging"

	"encoding/json"

	"github.com/knative/eventing-contrib/contrib/alimns/pkg/apis/sources/v1alpha1"
	sourcesv1alpha1 "github.com/knative/eventing-contrib/contrib/alimns/pkg/apis/sources/v1alpha1"
	"github.com/knative/eventing-contrib/contrib/alimns/pkg/reconciler"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Adapter implements the mns  adapter to deliver messages from
// a pre-existing topic/subscription to a Sink.
type Adapter struct {
	//
	K8sClient kubernetes.Interface
	//
	Namespace string
	//
	SecretName string
	//
	SecretKey string
	// TopicID is the pre-existing mns topic id to use.
	TopicID string
	// SubscriptionID is the pre-existing mns subscription id to use.
	SubscriptionID string
	// SinkURI is the URI messages will be forwarded on to.
	SinkURI string
	// TransformerURI is the URI messages will be forwarded on to for any transformation
	// before they are sent to SinkURI.
	TransformerURI string

	source string
	client alimns.MNSClient

	ceClient cloudevents.Client

	transformer       bool
	transformerClient cloudevents.Client
}

func (a *Adapter) Start(ctx context.Context) error {
	a.source = sourcesv1alpha1.AliMnsEventSource(a.TopicID)

	var err error
	secret, err := a.K8sClient.CoreV1().Secrets(a.Namespace).Get(a.SecretName, v1.GetOptions{})
	if err != nil {
		logging.FromContext(ctx).Desugar().Info("Failed to get secret", zap.Error(err))
		return err
	}
	cred := &v1alpha1.Credentials{}
	err = json.Unmarshal(secret.Data[a.SecretKey], cred)
	if err != nil {
		logging.FromContext(ctx).Desugar().Info("Failed to get secret", zap.Error(err))
		return err
	}

	if a.client, err = alimns.AliMNSClientCreator(ctx, cred); err != nil {
		return err
	}

	if a.ceClient == nil {
		if a.ceClient, err = kncloudevents.NewDefaultClient(a.SinkURI); err != nil {
			return fmt.Errorf("failed to create cloudevent client: %s", err.Error())
		}
	}

	// Make the transformer client in case the TransformerURI has been set.
	if a.TransformerURI != "" {
		a.transformer = true
		if a.transformerClient == nil {
			if a.transformerClient, err = kncloudevents.NewDefaultClient(a.TransformerURI); err != nil {
				return fmt.Errorf("failed to create transformer client: %s", err.Error())
			}
		}
	}

	return a.client.SubscriptionReceiveMessage(
		ctx,
		a.SubscriptionID,
		a.receiveMessage,
	)
}

func (a *Adapter) receiveMessage(ctx context.Context, m alimns.MNSMessage) {
	logger := logging.FromContext(ctx)
	logger.Infow("Received message", zap.Any("messageData", m.Data()))

	err := a.postMessage(ctx, logger, m)
	if err != nil {
		logger.Infof("Failed to post message: %s", err)
		m.Nack()
	} else {
		logger.Debug("Message sent successfully")
		m.Ack()
	}
}
func (a *Adapter) postMessage(ctx context.Context, logger *zap.SugaredLogger, m alimns.MNSMessage) error {
	// Create the CloudEvent.
	event := cloudevents.NewEvent(cloudevents.VersionV02)
	event.SetID(m.ID())
	event.SetTime(time.Unix(0, m.PublishTime()*int64(time.Millisecond)))
	event.SetDataContentType(*cloudevents.StringOfApplicationJSON())
	event.SetSource(a.source)
	event.SetData(m.Data())
	event.SetType(sourcesv1alpha1.AliMnsSourceEventType)

	// If a transformer has been configured, then transform the message.
	if a.transformer {
		resp, err := a.transformerClient.Send(ctx, event)
		if err != nil {
			logger.Errorf("error transforming cloud event %q", event.ID())
			return err
		}
		if resp == nil {
			logger.Warnf("cloud event %q was not transformed", event.ID())
			return nil
		}
		// Update the event with the transformed one.
		event = *resp
	}

	_, err := a.ceClient.Send(ctx, event)
	return err // err could be nil or an error
}
