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

// Package datasource defines the DataSource interface that decouples checkers
// and controllers from direct Kubernetes client usage, enabling pluggable
// backends (e.g., cached multi-cluster data from a console backend).
package datasource

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DataSource abstracts read-only data access for checkers and controllers.
// It mirrors controller-runtime's client.Reader interface so existing call
// sites require only a field rename, not logic changes.
type DataSource interface {
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}
