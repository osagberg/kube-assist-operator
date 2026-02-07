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
	"testing"
)

func TestNewEvaluator(t *testing.T) {
	eval, err := NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}
	if eval == nil {
		t.Fatal("NewEvaluator() returned nil")
	}
}

func TestEvaluator_Compile(t *testing.T) {
	eval, err := NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}

	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{
			name:    "valid boolean expression",
			expr:    "object.status.ready == false",
			wantErr: false,
		},
		{
			name:    "valid comparison expression",
			expr:    "object.spec.replicas > 1",
			wantErr: false,
		},
		{
			name:    "valid has() check",
			expr:    "has(object.status)",
			wantErr: false,
		},
		{
			name:    "invalid syntax",
			expr:    "object.status.ready ==",
			wantErr: true,
		},
		{
			name:    "empty expression",
			expr:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := eval.Compile(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("Compile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && compiled == nil {
				t.Error("Compile() returned nil for valid expression")
			}
		})
	}
}

func TestEvaluator_Evaluate(t *testing.T) {
	eval, err := NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}

	tests := []struct {
		name    string
		expr    string
		object  map[string]any
		want    bool
		wantErr bool
	}{
		{
			name: "condition true - replicas mismatch",
			expr: "object.status.readyReplicas < object.spec.replicas",
			object: map[string]any{
				"spec":   map[string]any{"replicas": int64(3)},
				"status": map[string]any{"readyReplicas": int64(1)},
			},
			want: true,
		},
		{
			name: "condition false - replicas match",
			expr: "object.status.readyReplicas < object.spec.replicas",
			object: map[string]any{
				"spec":   map[string]any{"replicas": int64(3)},
				"status": map[string]any{"readyReplicas": int64(3)},
			},
			want: false,
		},
		{
			name: "string comparison",
			expr: `object.status.phase != "Running"`,
			object: map[string]any{
				"status": map[string]any{"phase": "Pending"},
			},
			want: true,
		},
		{
			name: "nested field access",
			expr: `object.metadata.name == "test"`,
			object: map[string]any{
				"metadata": map[string]any{"name": "test"},
			},
			want: true,
		},
		{
			name:    "missing field causes error",
			expr:    "object.nonexistent.field > 0",
			object:  map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := eval.Compile(tt.expr)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			got, err := eval.Evaluate(compiled, tt.object)
			if (err != nil) != tt.wantErr {
				t.Errorf("Evaluate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluator_EvaluateString(t *testing.T) {
	eval, err := NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}

	t.Run("valid string expression", func(t *testing.T) {
		compiled, err := eval.Compile(`"hello " + object.metadata.name`)
		if err != nil {
			t.Fatalf("Compile() error = %v", err)
		}

		obj := map[string]any{
			"metadata": map[string]any{"name": "world"},
		}

		got, err := eval.EvaluateString(compiled, obj)
		if err != nil {
			t.Fatalf("EvaluateString() error = %v", err)
		}
		if got != "hello world" {
			t.Errorf("EvaluateString() = %q, want %q", got, "hello world")
		}
	})

	t.Run("non-string expression returns error", func(t *testing.T) {
		compiled, err := eval.Compile("object.spec.replicas > 1")
		if err != nil {
			t.Fatalf("Compile() error = %v", err)
		}

		obj := map[string]any{
			"spec": map[string]any{"replicas": int64(3)},
		}

		_, err = eval.EvaluateString(compiled, obj)
		if err == nil {
			t.Fatal("EvaluateString() should return error for non-string result")
		}
	})

	t.Run("eval error for missing field", func(t *testing.T) {
		compiled, err := eval.Compile("object.nonexistent.field")
		if err != nil {
			t.Fatalf("Compile() error = %v", err)
		}

		obj := map[string]any{}

		_, err = eval.EvaluateString(compiled, obj)
		if err == nil {
			t.Fatal("EvaluateString() should return error for missing field")
		}
	})
}

func TestEvaluator_EvaluateMessage(t *testing.T) {
	eval, err := NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}

	tests := []struct {
		name    string
		msgExpr string
		object  map[string]any
		want    string
		wantErr bool
	}{
		{
			name:    "static message",
			msgExpr: "Something is wrong",
			object:  map[string]any{},
			want:    "Something is wrong",
		},
		{
			name:    "CEL message expression",
			msgExpr: `="Deployment " + object.metadata.name + " has issues"`,
			object: map[string]any{
				"metadata": map[string]any{"name": "my-app"},
			},
			want: "Deployment my-app has issues",
		},
		{
			name:    "CEL message with string conversion",
			msgExpr: `="Resource " + object.metadata.name + " in namespace " + object.metadata.namespace`,
			object: map[string]any{
				"metadata": map[string]any{
					"name":      "test-deploy",
					"namespace": "production",
				},
			},
			want: "Resource test-deploy in namespace production",
		},
		{
			name:    "invalid CEL message",
			msgExpr: `="missing " + object.nonexistent.field`,
			object:  map[string]any{},
			wantErr: true,
		},
		{
			name:    "empty static message",
			msgExpr: "",
			object:  map[string]any{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := eval.EvaluateMessage(tt.msgExpr, tt.object)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateMessage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("EvaluateMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
