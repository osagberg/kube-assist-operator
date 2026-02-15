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

package flux

import "time"

// parseReconcileRequestTime parses Flux reconcile-request timestamps.
// Flux typically emits RFC3339Nano, but accept RFC3339 for robustness.
func parseReconcileRequestTime(v string) (time.Time, bool) {
	if v == "" {
		return time.Time{}, false
	}

	t, err := time.Parse(time.RFC3339Nano, v)
	if err == nil {
		return t, true
	}

	t, err = time.Parse(time.RFC3339, v)
	if err == nil {
		return t, true
	}

	return time.Time{}, false
}
