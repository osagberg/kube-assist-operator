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

package workload

// TODO(agent-3): delegate diagnosePodDetailed in troubleshootrequest_controller.go
// to workload.DiagnosePods() to eliminate the duplicated pod-diagnosis logic.
// The controller's diagnosePodDetailed function mirrors Checker.diagnosePod
// but adds extra detail for TroubleshootRequest output. Consider factoring a
// shared core and letting the controller add its extra fields on top.
