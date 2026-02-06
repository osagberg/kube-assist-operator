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

package causal

import (
	"fmt"
	"strings"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

const checkerWorkloads = "workloads"

// CrossCheckerRule defines a pattern that matches issues from two or more
// checkers and produces a CausalGroup with an inferred root cause.
type CrossCheckerRule struct {
	// Name is a human-readable identifier for this rule.
	Name string

	// Match returns true when the given issue matches one side of this rule.
	// The tag returned is used to pair matching issues (e.g., "oom" or "quota").
	Match func(checkerName string, issue checker.Issue) (tag string, ok bool)

	// RequiredTags lists the tags that must ALL be present for the rule to fire.
	RequiredTags []string

	// RootCause is a template string describing the inferred cause.
	// It may contain %s for the namespace.
	RootCause string

	// Confidence is the base confidence for matches of this rule.
	Confidence float64
}

// DefaultRules returns the built-in cross-checker correlation rules.
func DefaultRules() []CrossCheckerRule {
	return []CrossCheckerRule{
		oomQuotaRule(),
		crashImagePullRule(),
		fluxChainRule(),
		pvcWorkloadRule(),
	}
}

// oomQuotaRule: OOMKilled pods + quota near limit in the same namespace.
func oomQuotaRule() CrossCheckerRule {
	return CrossCheckerRule{
		Name:         "oom-quota",
		RequiredTags: []string{"oom", "quota"},
		RootCause:    "OOM kills are likely caused by resource quota pressure in namespace %s",
		Confidence:   0.85,
		Match: func(checkerName string, issue checker.Issue) (string, bool) {
			if checkerName == checkerWorkloads && strings.Contains(strings.ToUpper(issue.Type), "OOM") {
				return "oom", true
			}
			if checkerName == "quotas" && (issue.Severity == checker.SeverityCritical || issue.Severity == checker.SeverityWarning) {
				return "quota", true
			}
			return "", false
		},
	}
}

// crashImagePullRule: CrashLoopBackOff + ImagePullBackOff in the same namespace.
func crashImagePullRule() CrossCheckerRule {
	return CrossCheckerRule{
		Name:         "crash-imagepull",
		RequiredTags: []string{"crash", "imagepull"},
		RootCause:    "CrashLoop may be caused by image pull failures in namespace %s — pods restarting with wrong/missing images",
		Confidence:   0.8,
		Match: func(checkerName string, issue checker.Issue) (string, bool) {
			if checkerName != "workloads" {
				return "", false
			}
			upper := strings.ToUpper(issue.Type)
			if strings.Contains(upper, "CRASHLOOP") {
				return "crash", true
			}
			if strings.Contains(upper, "IMAGEPULL") {
				return "imagepull", true
			}
			return "", false
		},
	}
}

// fluxChainRule: Flux source failure + Kustomization/HelmRelease failure.
func fluxChainRule() CrossCheckerRule {
	return CrossCheckerRule{
		Name:         "flux-chain",
		RequiredTags: []string{"flux-source", "flux-consumer"},
		RootCause:    "Flux delivery pipeline failure in namespace %s — source issue is blocking downstream reconciliation",
		Confidence:   0.9,
		Match: func(checkerName string, issue checker.Issue) (string, bool) {
			if issue.Severity != checker.SeverityCritical && issue.Severity != checker.SeverityWarning {
				return "", false
			}
			if checkerName == "gitrepositories" {
				return "flux-source", true
			}
			if checkerName == "helmreleases" || checkerName == "kustomizations" {
				return "flux-consumer", true
			}
			return "", false
		},
	}
}

// pvcWorkloadRule: PVC pending/lost + workload issues in the same namespace.
func pvcWorkloadRule() CrossCheckerRule {
	return CrossCheckerRule{
		Name:         "pvc-workload",
		RequiredTags: []string{"pvc", "workload-pending"},
		RootCause:    "Workload scheduling issues in namespace %s may be caused by PVC binding failures — pods can't start until volumes are bound",
		Confidence:   0.75,
		Match: func(checkerName string, issue checker.Issue) (string, bool) {
			if checkerName == "pvcs" && (issue.Severity == checker.SeverityCritical || issue.Severity == checker.SeverityWarning) {
				return "pvc", true
			}
			if checkerName == checkerWorkloads {
				upper := strings.ToUpper(issue.Type)
				if strings.Contains(upper, "PENDING") || strings.Contains(upper, "UNSCHEDULABLE") {
					return "workload-pending", true
				}
			}
			return "", false
		},
	}
}

// applyCrossCheckerRules runs all rules against the input and returns matching groups.
func applyCrossCheckerRules(rules []CrossCheckerRule, input CorrelationInput) []CausalGroup {
	// Group all issues by namespace.
	type taggedEvent struct {
		event TimelineEvent
		tag   string
	}
	byNSAndRule := make(map[string]map[string][]taggedEvent) // ns -> ruleName -> events

	for _, rule := range rules {
		for checkerName, result := range input.Results {
			if result == nil || result.Error != nil {
				continue
			}
			for _, issue := range result.Issues {
				tag, ok := rule.Match(checkerName, issue)
				if !ok {
					continue
				}
				ns := issue.Namespace
				if ns == "" {
					ns = clusterScope
				}
				if byNSAndRule[ns] == nil {
					byNSAndRule[ns] = make(map[string][]taggedEvent)
				}
				byNSAndRule[ns][rule.Name] = append(byNSAndRule[ns][rule.Name], taggedEvent{
					event: TimelineEvent{
						Timestamp: input.Timestamp,
						Checker:   checkerName,
						Issue:     issue,
					},
					tag: tag,
				})
			}
		}
	}

	var groups []CausalGroup
	for ns, ruleEvents := range byNSAndRule {
		for _, rule := range rules {
			tagged := ruleEvents[rule.Name]
			if len(tagged) == 0 {
				continue
			}

			// Check that all required tags are present.
			tagSet := make(map[string]bool)
			for _, te := range tagged {
				tagSet[te.tag] = true
			}
			allPresent := true
			for _, required := range rule.RequiredTags {
				if !tagSet[required] {
					allPresent = false
					break
				}
			}
			if !allPresent {
				continue
			}

			var events []TimelineEvent
			for _, te := range tagged {
				events = append(events, te.event)
			}

			rootCause := fmt.Sprintf(rule.RootCause, ns)
			sev := highestSeverity(events)

			groups = append(groups, CausalGroup{
				ID:         fmt.Sprintf("rule-%s-%s-%d", rule.Name, ns, input.Timestamp.UnixMilli()),
				Title:      fmt.Sprintf("%s: %d issues in %s", rule.Name, len(events), ns),
				RootCause:  rootCause,
				Severity:   sev,
				Events:     events,
				Rule:       rule.Name,
				Confidence: rule.Confidence,
				FirstSeen:  input.Timestamp,
				LastSeen:   input.Timestamp,
			})
		}
	}
	return groups
}
