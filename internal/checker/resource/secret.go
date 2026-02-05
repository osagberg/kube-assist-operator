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

package resource

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

const (
	// SecretCheckerName is the identifier for this checker
	SecretCheckerName = "secrets"

	// Default warning threshold for cert expiry (30 days)
	DefaultCertExpiryWarningDays = 30
)

// SecretChecker analyzes Kubernetes Secrets for health issues
type SecretChecker struct {
	certExpiryWarningDays int
	checkCertExpiry       bool
}

// NewSecretChecker creates a new Secret checker
func NewSecretChecker() *SecretChecker {
	return &SecretChecker{
		certExpiryWarningDays: DefaultCertExpiryWarningDays,
		checkCertExpiry:       true,
	}
}

// WithCertExpiryWarningDays sets the cert expiry warning threshold
func (c *SecretChecker) WithCertExpiryWarningDays(days int) *SecretChecker {
	c.certExpiryWarningDays = days
	return c
}

// Name returns the checker identifier
func (c *SecretChecker) Name() string {
	return SecretCheckerName
}

// Supports always returns true since Secrets are core resources
func (c *SecretChecker) Supports(ctx context.Context, cl client.Client) bool {
	return true
}

// Check performs health checks on Secrets
func (c *SecretChecker) Check(ctx context.Context, checkCtx *checker.CheckContext) (*checker.CheckResult, error) {
	result := &checker.CheckResult{
		CheckerName: SecretCheckerName,
		Issues:      []checker.Issue{},
	}

	// Read config into local variables to avoid mutating shared state
	certExpiryWarningDays := c.certExpiryWarningDays
	checkCertExpiry := c.checkCertExpiry
	if checkCtx.Config != nil {
		if days, ok := checkCtx.Config["certExpiryWarningDays"].(int); ok {
			certExpiryWarningDays = days
		}
		if expiry, ok := checkCtx.Config["checkCertExpiry"].(bool); ok {
			checkCertExpiry = expiry
		}
	}

	for _, ns := range checkCtx.Namespaces {
		var secretList corev1.SecretList
		if err := checkCtx.Client.List(ctx, &secretList, client.InNamespace(ns)); err != nil {
			continue
		}

		for _, secret := range secretList.Items {
			issues := c.checkSecretWith(&secret, certExpiryWarningDays, checkCertExpiry)
			if len(issues) == 0 {
				result.Healthy++
			} else {
				result.Issues = append(result.Issues, issues...)
			}
		}
	}

	return result, nil
}

// checkSecretWith analyzes a single Secret using the provided config values
func (c *SecretChecker) checkSecretWith(secret *corev1.Secret, certExpiryWarningDays int, checkCertExpiry bool) []checker.Issue {
	var issues []checker.Issue
	resourceRef := fmt.Sprintf("secret/%s", secret.Name)

	// Check TLS secrets for certificate expiry
	if checkCertExpiry && secret.Type == corev1.SecretTypeTLS {
		certIssues := c.checkTLSCertificateWith(secret, resourceRef, certExpiryWarningDays)
		issues = append(issues, certIssues...)
	}

	// Check for kubernetes.io/tls type secrets that might have cert data
	if checkCertExpiry && secret.Type == corev1.SecretTypeOpaque {
		// Check if it looks like a TLS cert secret
		if _, hasCert := secret.Data["tls.crt"]; hasCert {
			certIssues := c.checkTLSCertificateWith(secret, resourceRef, certExpiryWarningDays)
			issues = append(issues, certIssues...)
		}
	}

	// Check for empty secrets (potential misconfiguration)
	if len(secret.Data) == 0 && len(secret.StringData) == 0 {
		// Skip service account tokens and other auto-generated secrets
		if secret.Type != corev1.SecretTypeServiceAccountToken {
			issues = append(issues, checker.Issue{
				Type:      "EmptySecret",
				Severity:  checker.SeverityWarning,
				Resource:  resourceRef,
				Namespace: secret.Namespace,
				Message:   fmt.Sprintf("Secret %s has no data", secret.Name),
				Suggestion: "This secret has no data keys. If it should contain data, populate it with: " +
					"kubectl create secret generic " + secret.Name + " --from-literal=key=value -n " + secret.Namespace + " --dry-run=client -o yaml | kubectl apply -f -. " +
					"Check if any workloads reference this secret: kubectl get pods -n " + secret.Namespace + " -o json | grep " + secret.Name + ".",
				Metadata: map[string]string{
					"secret": secret.Name,
					"type":   string(secret.Type),
				},
			})
		}
	}

	return issues
}

// checkTLSCertificateWith parses and validates TLS certificates using the provided warning threshold
func (c *SecretChecker) checkTLSCertificateWith(secret *corev1.Secret, resourceRef string, certExpiryWarningDays int) []checker.Issue {
	var issues []checker.Issue

	certData, ok := secret.Data["tls.crt"]
	if !ok {
		return issues
	}

	// Parse PEM encoded certificate
	block, _ := pem.Decode(certData)
	if block == nil {
		issues = append(issues, checker.Issue{
			Type:       "InvalidCertificate",
			Severity:   checker.SeverityWarning,
			Resource:   resourceRef,
			Namespace:  secret.Namespace,
			Message:    "Failed to parse TLS certificate PEM data",
			Suggestion: "Verify the certificate is properly PEM encoded",
			Metadata: map[string]string{
				"secret": secret.Name,
			},
		})
		return issues
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		issues = append(issues, checker.Issue{
			Type:       "InvalidCertificate",
			Severity:   checker.SeverityWarning,
			Resource:   resourceRef,
			Namespace:  secret.Namespace,
			Message:    fmt.Sprintf("Failed to parse certificate: %v", err),
			Suggestion: "Verify the certificate data is valid",
			Metadata: map[string]string{
				"secret": secret.Name,
			},
		})
		return issues
	}

	now := time.Now()
	warningThreshold := now.AddDate(0, 0, certExpiryWarningDays)

	// Check if certificate is expired
	if now.After(cert.NotAfter) {
		issues = append(issues, checker.Issue{
			Type:      "CertExpired",
			Severity:  checker.SeverityCritical,
			Resource:  resourceRef,
			Namespace: secret.Namespace,
			Message:   fmt.Sprintf("TLS certificate expired on %s", cert.NotAfter.Format("2006-01-02")),
			Suggestion: "This TLS certificate has expired and must be renewed immediately. " +
				"If using cert-manager, check the Certificate resource: kubectl get certificate -n " + secret.Namespace + ". " +
				"Force renewal: kubectl delete secret " + secret.Name + " -n " + secret.Namespace + " (cert-manager will recreate it). " +
				"If manually managed, generate and apply a new certificate.",
			Metadata: map[string]string{
				"secret":  secret.Name,
				"expiry":  cert.NotAfter.Format(time.RFC3339),
				"subject": cert.Subject.CommonName,
				"issuer":  cert.Issuer.CommonName,
			},
		})
	} else if cert.NotAfter.Before(warningThreshold) {
		// Check if certificate is expiring soon
		daysUntilExpiry := int(cert.NotAfter.Sub(now).Hours() / 24)
		issues = append(issues, checker.Issue{
			Type:      "CertExpiringSoon",
			Severity:  checker.SeverityWarning,
			Resource:  resourceRef,
			Namespace: secret.Namespace,
			Message:   fmt.Sprintf("TLS certificate expires in %d days (on %s)", daysUntilExpiry, cert.NotAfter.Format("2006-01-02")),
			Suggestion: "This TLS certificate is expiring soon. Plan renewal before expiry to avoid downtime. " +
				"If using cert-manager: kubectl get certificate -n " + secret.Namespace + " to check auto-renewal status. " +
				"If manually managed, generate a new certificate and update the secret.",
			Metadata: map[string]string{
				"secret":        secret.Name,
				"expiry":        cert.NotAfter.Format(time.RFC3339),
				"daysRemaining": fmt.Sprintf("%d", daysUntilExpiry),
				"subject":       cert.Subject.CommonName,
				"issuer":        cert.Issuer.CommonName,
			},
		})
	}

	// Check if certificate is not yet valid
	if now.Before(cert.NotBefore) {
		issues = append(issues, checker.Issue{
			Type:       "CertNotYetValid",
			Severity:   checker.SeverityWarning,
			Resource:   resourceRef,
			Namespace:  secret.Namespace,
			Message:    fmt.Sprintf("TLS certificate is not valid until %s", cert.NotBefore.Format("2006-01-02")),
			Suggestion: "Verify the certificate validity period is correct",
			Metadata: map[string]string{
				"secret":    secret.Name,
				"validFrom": cert.NotBefore.Format(time.RFC3339),
				"subject":   cert.Subject.CommonName,
			},
		})
	}

	return issues
}
