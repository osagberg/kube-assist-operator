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

package checker

import "regexp"

// PodHashSuffix matches the standard Kubernetes pod hash suffixes.
// Pod names follow the pattern: {rs-name}-{5-char alphanumeric} where rs-name
// is {deployment-name}-{8-10 char hash}. This regex matches both the RS and
// pod-template suffixes, e.g. "-7f8b4c5d9f-x2k4p" or just "-abc12".
var PodHashSuffix = regexp.MustCompile(`-[a-f0-9]{5,}(-[a-z0-9]{5})?`)

// RSHashSuffix matches the standard Kubernetes ReplicaSet hash suffix.
// ReplicaSet names follow: {deployment-name}-{8-10 char alphanumeric hash}
var RSHashSuffix = regexp.MustCompile(`-[a-z0-9]{8,10}$`)
