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

package main

import (
	"flag"
	"log"

	"fmt"

	"github.com/kelseyhightower/envconfig"
	mns "github.com/knative/eventing-contrib/contrib/alitablestore/pkg/adapter"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"golang.org/x/net/context"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type envConfig struct {
	// Environment variable containing the NAMESPACE
	Namespace string `envconfig:"NAMESPACE" required:"true"`
	// Environment variable containing the SECRET_NAME
	SecretName string `envconfig:"SECRET_NAME" required:"true"`
	// Environment variable containing the SECRET_KEY.
	SecretKey string `envconfig:"SECRET_KEY" required:"true"`
	// Environment variable containing the sink URI.
	Sink string `envconfig:"SINK_URI" required:"true"`

	// Environment variable containing the transformer URI.
	Transformer string `envconfig:"TRANSFORMER_URI" default:""`

	// Environment variable containing the tablestore table name being
	TableName string `envconfig:"TABLE_NAME" default:""`
	INSTANCE  string `envconfig:"INSTANCE" default:""`
	// Environment variable containing the name of the subscription to use.
	Subscription string `envconfig:"SUBSCRIPTION_ID" required:"true"`
}

func main() {
	flag.Parse()

	ctx := context.Background()
	logCfg := zap.NewProductionConfig()
	logCfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	logger, err := logCfg.Build()
	if err != nil {
		log.Fatalf("Unable to create logger: %v", err)
	}

	var env envConfig
	if err := envconfig.Process("", &env); err != nil {
		logger.Fatal("Failed to process env var", zap.Error(err))
	}
	client, err := newKubernetesClient()
	if err != nil {
		logger.Fatal("Failed to initialize kubernetes client: ", zap.Error(err))
	}

	adapter := &mns.Adapter{
		Namespace:      env.Namespace,
		SecretName:     env.SecretName,
		SecretKey:      env.SecretKey,
		K8sClient:      client,
		TableName:      env.TableName,
		Instance:       env.INSTANCE,
		SinkURI:        env.Sink,
		SubscriptionID: env.Subscription,
		TransformerURI: env.Transformer,
	}

	logger.Info("Starting AliTablestore Receive Adapter. %v", zap.Reflect("adapter: ", adapter))
	if err := adapter.Start(ctx); err != nil {
		logger.Fatal("failed to start adapter: ", zap.Error(err))
	}
}
func newKubernetesClient() (kubernetes.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create in-cluster config: %v", err)
	}
	return kubernetes.NewForConfig(config)
}
