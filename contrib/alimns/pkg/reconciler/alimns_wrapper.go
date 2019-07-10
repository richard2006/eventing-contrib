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

package alimns

import (
	"context"

	"encoding/json"
	"strings"

	"fmt"

	"github.com/knative/eventing-contrib/contrib/alimns/pkg/apis/sources/v1alpha1"
	"github.com/souriki/ali_mns"
	"golang.org/x/sync/errgroup"
)

// This file exists so that we can unit test failures with the PubSub client.

type AliMNSMessageWrapper struct {
}

func (msw *AliMNSMessageWrapper) GetMessagePublishRequest() (*ali_mns.MessagePublishRequest, error) {
	bytes, err := json.Marshal(msw)
	if err != nil {
		return nil, err
	}
	msg := &ali_mns.MessagePublishRequest{
		MessageBody: fmt.Sprintf("%s", bytes),
	}

	return msg, nil
}

// MNSClientCreator creates a MNSClient.
type MNSClientCreator func(ctx context.Context, creds *v1alpha1.Credentials) (MNSClient, error)

// AliMNSClientCreator creates a real MNS client. It should always be used, except during
// unit tests.
func AliMNSClientCreator(ctx context.Context, creds *v1alpha1.Credentials) (MNSClient, error) {
	client := ali_mns.NewAliMNSClient(creds.Url, creds.AccessKeyId, creds.AccessKeySecret)
	return &realAliMNSClient{client: &client}, nil
}

// MNSClient is the set of methods we use on mns.Client. See mns.Client for documentation
// of the functions.
type MNSClient interface {
	//SubscriptionInProject(id string) PubSubSubscription

	CreateTopic(ctx context.Context, topicName string) error
	DeleteTopic(ctx context.Context, topicName string) error
	TopicExists(ctx context.Context, topicName string) (bool, error)
	//TopicPublish(ctx context.Context)
	Topic(ctx context.Context, topicName string) (ali_mns.AliMNSTopic, error)

	CreateSubscription(ctx context.Context, topicName, subscriptionName string) error
	DeleteSubscription(ctx context.Context, topicName, subscriptionName string) error
	SubscriptionExists(ctx context.Context, topicName, subscriptionName string) (bool, error)
	SubscriptionReceiveMessage(ctx context.Context, subscriptionName string, f func(context.Context, MNSMessage)) error
}

// realAliMNSClient is the client that will be used everywhere except unit tests. Verify that it
// satisfies the interface.
var _ MNSClient = &realAliMNSClient{}

// realAliMNSClient wraps a real MNS client, so that it matches the MNSClient
// interface. It is needed because the real SubscriptionInProject returns a struct and does not
// implicitly match gcpMNSClient, which returns an interface.
type realAliMNSClient struct {
	client *ali_mns.MNSClient
}

func (c *realAliMNSClient) Topic(ctx context.Context, topicName string) (ali_mns.AliMNSTopic, error) {
	return ali_mns.NewMNSTopic(topicName, *c.client), nil

}
func (c *realAliMNSClient) TopicExists(ctx context.Context, topicName string) (bool, error) {
	topicManager := ali_mns.NewMNSTopicManager(*c.client)
	_, err := topicManager.GetTopicAttributes(topicName)
	if err != nil {
		if ali_mns.ERR_MNS_TOPIC_NOT_EXIST.IsEqual(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (c *realAliMNSClient) SubscriptionReceiveMessage(ctx context.Context, subscriptionName string, f func(context.Context, MNSMessage)) error {
	group, gctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return c.subscriptionReceiver(gctx, subscriptionName, f)
	})
	return group.Wait()
}

func (c *realAliMNSClient) subscriptionReceiver(ctx context.Context, subscriptionName string, f func(context.Context, MNSMessage)) error {
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()

	queue := ali_mns.NewMNSQueue(subscriptionName, *c.client)
	for {
		respChan := make(chan ali_mns.MessageReceiveResponse, 1)
		errChan := make(chan error, 1)

		queue.ReceiveMessage(respChan, errChan, 30)
		select {
		case resp := <-respChan:
			{
				msg := &realAliMNSMessage{
					mrResp:       &resp,
					client:       c.client,
					subscription: subscriptionName,
				}
				f(ctx2, msg)
			}
		case err := <-errChan:
			{
				// if no message receive agen
				if strings.Contains(err.Error(), "MessageNotExist") {
					continue
				}

			}
		}
	}

	return nil
}

func (c *realAliMNSClient) SubscriptionExists(ctx context.Context, topicName, subscriptionName string) (bool, error) {
	topicManager := ali_mns.NewMNSQueueManager(*c.client)
	_, err := topicManager.GetQueueAttributes(subscriptionName)
	if err != nil {
		if ali_mns.ERR_MNS_QUEUE_NOT_EXIST.IsEqual(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (c *realAliMNSClient) DeleteSubscription(ctx context.Context, topicName, subscriptionName string) error {
	queueManager := ali_mns.NewMNSQueueManager(*c.client)

	if err := queueManager.DeleteQueue(subscriptionName); err != nil {
		if ali_mns.ERR_MNS_QUEUE_NOT_EXIST.IsEqual(err) {
			return nil
		}

		return err
	}

	topic := ali_mns.NewMNSTopic(topicName, *c.client)

	err := topic.Unsubscribe(subscriptionName)
	if err != nil && !ali_mns.ERR_MNS_INVALID_SUBSCRIPTION_NAME.IsEqual(err) {
		return err
	}

	return nil
}

func (c *realAliMNSClient) CreateSubscription(ctx context.Context, topicName, subscriptionName string) error {
	queueManager := ali_mns.NewMNSQueueManager(*c.client)

	err := queueManager.CreateQueue(subscriptionName, 0, 65536, 345600, 30, 0, 3)

	if err != nil && !ali_mns.ERR_MNS_QUEUE_ALREADY_EXIST_AND_HAVE_SAME_ATTR.IsEqual(err) {
		return err
	}

	topic := ali_mns.NewMNSTopic(topicName, *c.client)
	sub := ali_mns.MessageSubsribeRequest{
		Endpoint:            topic.GenerateQueueEndpoint(subscriptionName),
		NotifyContentFormat: ali_mns.SIMPLIFIED,
	}

	err = topic.Subscribe(subscriptionName, sub)
	if err != nil && !ali_mns.ERR_MNS_SUBSCRIPTION_ALREADY_EXIST_AND_HAVE_SAME_ATTR.IsEqual(err) {
		return err
	}
	return nil
}

func (c *realAliMNSClient) CreateTopic(ctx context.Context, topicName string) error {
	topicManager := ali_mns.NewMNSTopicManager(*c.client)
	err := topicManager.CreateSimpleTopic(topicName)
	if err != nil && !ali_mns.ERR_MNS_TOPIC_ALREADY_EXIST_AND_HAVE_SAME_ATTR.IsEqual(err) {
		return err
	}

	return nil
}

func (c *realAliMNSClient) DeleteTopic(ctx context.Context, topicName string) error {
	topicManager := ali_mns.NewMNSTopicManager(*c.client)
	err := topicManager.DeleteTopic(topicName)
	if err != nil && !ali_mns.ERR_MNS_TOPIC_NOT_EXIST.IsEqual(err) {
		return err
	}

	return nil
}

// MNSMessage is the set of methods we use on pubsub.Message. It exists to make MNSClient unit
// testable. See pubsub.Message for documentation of the functions.
type MNSMessage interface {
	ID() string
	PublishTime() int64
	Ack() error
	Nack() error
	Data() string
	MessageResponse() *ali_mns.MessageReceiveResponse
}

// realAliMNSMessage wraps a real MNS Message, so that it matches the MNSMessage
// interface.
type realAliMNSMessage struct {
	AliMNSMessageWrapper *AliMNSMessageWrapper

	mrResp       *ali_mns.MessageReceiveResponse
	client       *ali_mns.MNSClient
	subscription string
}

// realAliMNSMessage is the real MNSMessage used everywhere except unit tests. Verify that it
// satisfies the interface.
var _ MNSMessage = &realAliMNSMessage{}

func (m *realAliMNSMessage) MessageResponse() *ali_mns.MessageReceiveResponse {
	return m.mrResp
}

func (m *realAliMNSMessage) ID() string {
	return m.mrResp.MessageId
}

func (m *realAliMNSMessage) Data() string {
	return m.mrResp.MessageBody
}

func (m *realAliMNSMessage) PublishTime() int64 {
	return m.mrResp.FirstDequeueTime
}

func (m *realAliMNSMessage) Ack() error {
	queue := ali_mns.NewMNSQueue(m.subscription, *m.client)
	if e := queue.DeleteMessage(m.mrResp.ReceiptHandle); e != nil {
		return e
	}

	return nil
}

func (m *realAliMNSMessage) Nack() error {
	queue := ali_mns.NewMNSQueue(m.subscription, *m.client)
	if _, e := queue.ChangeMessageVisibility(m.mrResp.ReceiptHandle, 3); e != nil {
		return e
	}
	return nil
}
