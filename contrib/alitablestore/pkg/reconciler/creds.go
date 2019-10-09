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

package alitablestore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/knative/eventing-contrib/contrib/alitablestore/pkg/apis/sources/v1alpha1"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/logging"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetCredentials gets MNS credentials from a secretRef. The credentials must be stored in JSON format
// in the secretRef.
func GetCredentials(ctx context.Context, client client.Client, src *v1alpha1.AliTablestoreSource) (*v1alpha1.Credentials, error) {
	if src.Spec.AccessToken.SecretKeyRef == nil {
		return nil, fmt.Errorf("nil secretKeyRef")
	}
	return GetCredentialsByName(ctx, client, src.Namespace, src.Spec.AccessToken.SecretKeyRef.Name, src.Spec.AccessToken.SecretKeyRef.Key)
}
func GetCredentialsByName(ctx context.Context, client client.Client, namespace, name, key string) (*v1alpha1.Credentials, error) {
	secret := &v1.Secret{}
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret)
	if err != nil {
		logging.FromContext(ctx).Error("Unable to read the secretRef", zap.Any("secret", name))
		return nil, err
	}

	bytes, present := secret.Data[key]
	if !present {
		logging.FromContext(ctx).Error("Secret did not contain the key", zap.String("key", key))
		return nil, fmt.Errorf("secretRef did not contain the key '%s'", key)
	}

	credentials := &v1alpha1.Credentials{}
	if err := json.Unmarshal(bytes, credentials); err != nil {
		logging.FromContext(ctx).Error("Unable to create the MNS credential", zap.Error(err))
		return nil, err
	}
	return credentials, nil
}
