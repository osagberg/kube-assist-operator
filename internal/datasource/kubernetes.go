/*
Copyright 2026.

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

package datasource

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KubernetesDataSource implements DataSource by delegating to a
// controller-runtime client.Reader (typically a manager's cached client).
type KubernetesDataSource struct {
	reader client.Reader
}

// NewKubernetes creates a DataSource backed by a controller-runtime Reader.
func NewKubernetes(reader client.Reader) *KubernetesDataSource {
	return &KubernetesDataSource{reader: reader}
}

func (k *KubernetesDataSource) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return k.reader.Get(ctx, key, obj, opts...)
}

func (k *KubernetesDataSource) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return k.reader.List(ctx, list, opts...)
}
