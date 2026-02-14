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

// Package plugin provides a CEL-based custom checker plugin system.
package plugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
)

// celCostLimit is the maximum runtime cost for CEL program evaluation.
// This prevents denial-of-service via expensive expressions.
const celCostLimit = 1_000_000

// celEvalTimeout is the maximum duration for a single CEL evaluation.
const celEvalTimeout = 30 * time.Second

// CompiledRule holds a pre-compiled CEL program ready for evaluation.
type CompiledRule struct {
	program cel.Program
}

// Evaluator compiles and evaluates CEL expressions against Kubernetes objects.
type Evaluator struct {
	env *cel.Env
}

// NewEvaluator creates a new CEL evaluator with an environment configured
// with an `object` variable of dynamic type for accessing unstructured K8s objects.
func NewEvaluator() (*Evaluator, error) {
	env, err := cel.NewEnv(
		cel.Variable("object", cel.DynType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}
	return &Evaluator{env: env}, nil
}

// Compile parses and type-checks a CEL expression, returning a compiled program.
func (e *Evaluator) Compile(expr string) (*CompiledRule, error) {
	ast, issues := e.env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("CEL compilation error: %w", issues.Err())
	}

	prg, err := e.env.Program(ast, cel.CostLimit(celCostLimit))
	if err != nil {
		return nil, fmt.Errorf("CEL program creation error: %w", err)
	}

	return &CompiledRule{program: prg}, nil
}

// Evaluate runs a compiled CEL expression against an unstructured object
// and returns whether the condition is true.
func (e *Evaluator) Evaluate(compiled *CompiledRule, object map[string]any) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), celEvalTimeout)
	defer cancel()

	out, _, err := compiled.program.ContextEval(ctx, map[string]any{
		"object": object,
	})
	if err != nil {
		return false, fmt.Errorf("CEL evaluation error: %w", err)
	}

	val, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("CEL expression did not return bool, got %T", out.Value())
	}
	return val, nil
}

// EvaluateString runs a compiled CEL expression and returns the string result.
func (e *Evaluator) EvaluateString(compiled *CompiledRule, object map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), celEvalTimeout)
	defer cancel()

	out, _, err := compiled.program.ContextEval(ctx, map[string]any{"object": object})
	if err != nil {
		return "", fmt.Errorf("CEL evaluation error: %w", err)
	}
	val, ok := out.Value().(string)
	if !ok {
		return "", fmt.Errorf("CEL expression did not return string, got %T", out.Value())
	}
	return val, nil
}

// EvaluateMessage evaluates a message expression. If the message starts with "=",
// it is treated as a CEL expression that should return a string.
// Otherwise it is returned as a static string.
func (e *Evaluator) EvaluateMessage(msgExpr string, object map[string]any) (string, error) {
	if !strings.HasPrefix(msgExpr, "=") {
		return msgExpr, nil
	}

	celExpr := strings.TrimPrefix(msgExpr, "=")
	ast, issues := e.env.Compile(celExpr)
	if issues != nil && issues.Err() != nil {
		return "", fmt.Errorf("CEL message compilation error: %w", issues.Err())
	}

	prg, err := e.env.Program(ast, cel.CostLimit(celCostLimit))
	if err != nil {
		return "", fmt.Errorf("CEL message program error: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), celEvalTimeout)
	defer cancel()

	out, _, err := prg.ContextEval(ctx, map[string]any{
		"object": object,
	})
	if err != nil {
		return "", fmt.Errorf("CEL message evaluation error: %w", err)
	}

	val, ok := out.Value().(string)
	if !ok {
		return "", fmt.Errorf("CEL message expression did not return string, got %T", out.Value())
	}
	return val, nil
}
