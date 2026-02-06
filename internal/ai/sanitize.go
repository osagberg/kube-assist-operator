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

package ai

import (
	"regexp"
	"strings"
)

// SensitivePatterns contains regex patterns for sensitive data
var SensitivePatterns = []*regexp.Regexp{
	// API keys and tokens
	regexp.MustCompile(`(?i)(api[_-]?key|apikey|token|secret|password|passwd|pwd|auth)[=:\s]+[^\s]+`),
	regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9\-_]+`),

	// AWS credentials
	regexp.MustCompile(`(?i)aws[_-]?(access[_-]?key|secret|session)[=:\s]+[^\s]+`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),

	// Base64 encoded data that might be secrets
	regexp.MustCompile(`(?i)(data|value|secret)[:\s]*[a-zA-Z0-9+/=]{40,}`),

	// Email addresses
	regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`),

	// IP addresses (internal)
	regexp.MustCompile(`\b(10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(1[6-9]|2[0-9]|3[0-1])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3})\b`),

	// Certificate/key content
	regexp.MustCompile(`-----BEGIN [A-Z ]+-----[\s\S]*?-----END [A-Z ]+-----`),

	// Connection strings
	regexp.MustCompile(`(?i)(mongodb|postgres|mysql|redis)://[^\s]+`),

	// JWT tokens
	regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`),

	// Kubernetes secrets reference
	regexp.MustCompile(`(?i)secretRef:[^\n]+`),
}

// RedactionPlaceholder is used to replace sensitive data
const RedactionPlaceholder = "[REDACTED]"

// Sanitizer provides methods to sanitize data before sending to AI
type Sanitizer struct {
	patterns           []*regexp.Regexp
	additionalPatterns []*regexp.Regexp
}

// NewSanitizer creates a new sanitizer with default patterns
func NewSanitizer() *Sanitizer {
	return &Sanitizer{
		patterns: SensitivePatterns,
	}
}

// AddPattern adds a custom pattern to the sanitizer
func (s *Sanitizer) AddPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	s.additionalPatterns = append(s.additionalPatterns, re)
	return nil
}

// SanitizeString removes sensitive data from a string
func (s *Sanitizer) SanitizeString(input string) string {
	result := input

	// Apply default patterns
	for _, pattern := range s.patterns {
		result = pattern.ReplaceAllString(result, RedactionPlaceholder)
	}

	// Apply additional patterns
	for _, pattern := range s.additionalPatterns {
		result = pattern.ReplaceAllString(result, RedactionPlaceholder)
	}

	return result
}

// SanitizeStrings sanitizes a slice of strings
func (s *Sanitizer) SanitizeStrings(inputs []string) []string {
	result := make([]string, len(inputs))
	for i, input := range inputs {
		result[i] = s.SanitizeString(input)
	}
	return result
}

// SanitizeIssueContext sanitizes an issue context for AI analysis
func (s *Sanitizer) SanitizeIssueContext(issue IssueContext) IssueContext {
	return IssueContext{
		Type:             issue.Type,
		Severity:         issue.Severity,
		Resource:         s.SanitizeResourceName(issue.Resource),
		Namespace:        issue.Namespace,
		Message:          s.SanitizeString(issue.Message),
		StaticSuggestion: issue.StaticSuggestion, // Don't sanitize suggestions
		Events:           s.SanitizeStrings(issue.Events),
		Logs:             s.SanitizeStrings(issue.Logs),
	}
}

// SanitizeResourceName removes potentially sensitive parts of resource names
func (s *Sanitizer) SanitizeResourceName(name string) string {
	// Keep the resource type and a generic name
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return name
	}

	// Sanitize the resource name but keep the type
	resourceType := parts[0]
	resourceName := parts[1]

	// Check for potentially sensitive name patterns
	sensitiveNamePatterns := []string{
		"secret",
		"token",
		"credential",
		"password",
		"key",
	}

	nameLower := strings.ToLower(resourceName)
	for _, pattern := range sensitiveNamePatterns {
		if strings.Contains(nameLower, pattern) {
			resourceName = resourceType + "-" + RedactionPlaceholder
			break
		}
	}

	return resourceType + "/" + resourceName
}

// SanitizeAnalysisRequest sanitizes an entire analysis request
func (s *Sanitizer) SanitizeAnalysisRequest(req AnalysisRequest) AnalysisRequest {
	sanitized := AnalysisRequest{
		ClusterContext: req.ClusterContext, // Cluster context is generally safe
		CausalContext:  req.CausalContext,  // Causal context is pre-sanitized
		MaxTokens:      req.MaxTokens,
	}

	sanitized.Issues = make([]IssueContext, len(req.Issues))
	for i, issue := range req.Issues {
		sanitized.Issues[i] = s.SanitizeIssueContext(issue)
	}

	return sanitized
}
