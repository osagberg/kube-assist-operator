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

package plugin

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

var log = logf.Log.WithName("plugin-checker")

// compiledCheck holds a pre-compiled rule and its optional compiled message program.
type compiledCheck struct {
	rule    assistv1alpha1.CheckRule
	program *CompiledRule
	msgProg *CompiledRule // nil if message is static
}

// PluginChecker implements the checker.Checker interface using CEL expressions
// defined in a CheckPlugin CRD.
type PluginChecker struct {
	name      string
	spec      assistv1alpha1.CheckPluginSpec
	evaluator *Evaluator
	compiled  []compiledCheck
}

// NewPluginChecker creates a PluginChecker by compiling all CEL rules from the spec.
// Returns an error if any rule fails to compile.
func NewPluginChecker(name string, spec assistv1alpha1.CheckPluginSpec) (*PluginChecker, error) {
	eval, err := NewEvaluator()
	if err != nil {
		return nil, fmt.Errorf("failed to create evaluator: %w", err)
	}

	compiled := make([]compiledCheck, 0, len(spec.Rules))
	for _, rule := range spec.Rules {
		prg, err := eval.Compile(rule.Condition)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", rule.Name, err)
		}

		cc := compiledCheck{
			rule:    rule,
			program: prg,
		}

		// Pre-compile message if it's a CEL expression
		if len(rule.Message) > 0 && rule.Message[0] == '=' {
			msgPrg, err := eval.Compile(rule.Message[1:])
			if err != nil {
				return nil, fmt.Errorf("rule %q message: %w", rule.Name, err)
			}
			cc.msgProg = msgPrg
		}

		compiled = append(compiled, cc)
	}

	return &PluginChecker{
		name:      name,
		spec:      spec,
		evaluator: eval,
		compiled:  compiled,
	}, nil
}

// Name returns the checker identifier, namespaced with "plugin:" prefix.
func (p *PluginChecker) Name() string {
	return "plugin:" + p.name
}

// Supports always returns true for plugin checkers since they define their own target.
func (p *PluginChecker) Supports(_ context.Context, _ datasource.DataSource) bool {
	return true
}

// Check performs the health check by listing target resources and evaluating
// each compiled CEL rule against each resource.
func (p *PluginChecker) Check(ctx context.Context, checkCtx *checker.CheckContext) (*checker.CheckResult, error) {
	if checkCtx == nil {
		return nil, fmt.Errorf("checkCtx must not be nil")
	}

	result := &checker.CheckResult{
		CheckerName: p.Name(),
		Issues:      []checker.Issue{},
	}

	gvr := p.spec.TargetResource

	// Track namespace errors
	var nsErrors []string

	for _, ns := range checkCtx.Namespaces {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   gvr.Group,
			Version: gvr.Version,
			Kind:    gvr.Kind + "List",
		})

		if err := checkCtx.DataSource.List(ctx, list, client.InNamespace(ns)); err != nil {
			log.Error(err, "Failed to list resources for plugin",
				"plugin", p.name, "gvk", fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Version, gvr.Kind), "namespace", ns)
			nsErrors = append(nsErrors, ns)
			result.Issues = append(result.Issues, checker.Issue{
				Type:      fmt.Sprintf("plugin:%s:list-error", p.name),
				Severity:  checker.SeverityWarning,
				Resource:  fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Version, gvr.Kind),
				Namespace: ns,
				Message:   fmt.Sprintf("Failed to list %s resources: %v", gvr.Kind, err),
			})
			continue
		}

		for _, item := range list.Items {
			obj := item.Object
			hasIssue := false

			for _, cc := range p.compiled {
				matched, err := p.evaluator.Evaluate(cc.program, obj)
				if err != nil {
					log.Error(err, "CEL evaluation error",
						"plugin", p.name, "rule", cc.rule.Name, "resource", item.GetName())
					result.Issues = append(result.Issues, checker.Issue{
						Type:      fmt.Sprintf("plugin:%s:eval-error", p.name),
						Severity:  checker.SeverityWarning,
						Resource:  fmt.Sprintf("%s/%s", gvr.Resource, item.GetName()),
						Namespace: ns,
						Message:   fmt.Sprintf("CEL rule %q evaluation failed: %v", cc.rule.Name, err),
					})
					continue
				}

				if !matched {
					continue
				}

				hasIssue = true

				var msg string
				var msgErr error
				if cc.msgProg != nil {
					msg, msgErr = p.evaluator.EvaluateString(cc.msgProg, obj)
				} else {
					msg = cc.rule.Message
				}
				if msgErr != nil {
					msg = fmt.Sprintf("Rule %q triggered (message evaluation failed: %v)", cc.rule.Name, msgErr)
				}

				severity := cc.rule.Severity
				if severity == "" {
					severity = checker.SeverityWarning
				} else if !isValidSeverity(severity) {
					severity = checker.SeverityWarning
					log.Info("Invalid severity in rule, defaulting to Warning",
						"plugin", p.name, "rule", cc.rule.Name, "severity", cc.rule.Severity)
				}

				result.Issues = append(result.Issues, checker.Issue{
					Type:      fmt.Sprintf("plugin:%s:%s", p.name, cc.rule.Name),
					Severity:  severity,
					Resource:  fmt.Sprintf("%s/%s", gvr.Resource, item.GetName()),
					Namespace: ns,
					Message:   msg,
				})
			}

			if !hasIssue {
				result.Healthy++
			}
		}
	}

	if len(nsErrors) == len(checkCtx.Namespaces) {
		result.Error = fmt.Errorf("all %d namespace(s) failed: %v", len(nsErrors), nsErrors)
	}

	return result, nil
}

func isValidSeverity(s string) bool {
	switch s {
	case checker.SeverityCritical, checker.SeverityWarning, checker.SeverityInfo:
		return true
	}
	return false
}
