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

package tablestore

import (
	"fmt"

	"time"

	cloudevents "github.com/cloudevents/sdk-go"
	"github.com/knative/eventing-contrib/pkg/kncloudevents"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"knative.dev/pkg/logging"

	"encoding/json"

	"github.com/aliyun/aliyun-tablestore-go-sdk/tunnel"
	"github.com/knative/eventing-contrib/contrib/alitablestore/pkg/apis/sources/v1alpha1"
	sourcesv1alpha1 "github.com/knative/eventing-contrib/contrib/alitablestore/pkg/apis/sources/v1alpha1"
	alitablestore "github.com/knative/eventing-contrib/contrib/alitablestore/pkg/reconciler"
	"golang.org/x/net/context"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Adapter implements the tablestore  adapter to deliver messages from
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
	// TableName is the pre-existing tablestore table to use.
	TableName string

	Instance string
	// SubscriptionID is the pre-existing tablestore subscription id to use.
	SubscriptionID string
	// SinkURI is the URI messages will be forwarded on to.
	SinkURI string

	AdCode string
	// TransformerURI is the URI messages will be forwarded on to for any transformation
	// before they are sent to SinkURI.
	TransformerURI string

	source string
	client alitablestore.TunnelClient

	ceClient          cloudevents.Client
	ctx               context.Context
	transformer       bool
	transformerClient cloudevents.Client
}

func (a *Adapter) Start(ctx context.Context) error {
	a.source = sourcesv1alpha1.AliTablestoreEventSource(a.TableName)
	a.ctx = ctx
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

	if a.client, err = alitablestore.AliTunnelClientCreator(ctx, a.Instance, cred); err != nil {
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
		a.TableName,
		a.SubscriptionID,
		a.receiveMessage,
	)
}

func (a *Adapter) receiveMessage(tunnelCtx *tunnel.ChannelContext, records []*tunnel.Record) error {
	logger := logging.FromContext(a.ctx).With(zap.Any("eventID", tunnelCtx.TunnelId), zap.Any("sink", a.SinkURI))
	if records == nil || len(records) == 0 {
		logger.Infow("No Received message")
	}
	recordByte, _ := json.Marshal(records)
	logger.Infow("Received message", zap.Any("messageData", string(recordByte)))

	err := a.postMessage(a.ctx, logger, tunnelCtx, records)
	if err != nil {
		logger.Infof("Failed to post message: %s", err)
	} else {
		logger.Debug("Message sent successfully")
	}
	return nil
}
func (a *Adapter) postMessage(ctx context.Context, logger *zap.SugaredLogger, tunnelCtx *tunnel.ChannelContext, records []*tunnel.Record) error {
	for _, r := range records {
		id := ""
		adcode := ""
		rMap := make(map[string]string, 0)
		if r.PrimaryKey.PrimaryKeys != nil {
			for _, col := range r.PrimaryKey.PrimaryKeys {
				rMap[col.ColumnName] = col.Value.(string)
			}
		}
		for _, col := range r.Columns {
			// 过滤掉 id 信息
			if *col.Name == "id" {
				id = col.Value.(string)
			}
			if *col.Name == "adcode" {
				adcode = col.Value.(string)
			}
			rMap[*col.Name] = col.Value.(string)
		}
		// Create the CloudEvent.
		event := cloudevents.NewEvent(cloudevents.VersionV02)
		event.SetID(id)
		event.SetTime(time.Now())
		event.SetDataContentType(*cloudevents.StringOfApplicationJSON())
		event.SetSource(a.source)
		event.SetData(rMap)
		event.SetType(sourcesv1alpha1.AliTablestoreEventType(adcode))

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
		if err != nil {
			logger.Errorf("error Send cloud event %s", err.Error())
		}
		continue
	}
	return nil
}
