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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/testutil"
)

// generateTestCert creates a test certificate with specified validity period
func generateTestCert(notBefore, notAfter time.Time) ([]byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test.example.com",
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return certPEM, nil
}

func TestSecretChecker_Name(t *testing.T) {
	c := NewSecretChecker()
	if c.Name() != SecretCheckerName {
		t.Errorf("Name() = %v, want %v", c.Name(), SecretCheckerName)
	}
}

func TestSecretChecker_Supports(t *testing.T) {
	ds := testutil.NewDataSource(t)
	c := NewSecretChecker()
	if !c.Supports(context.Background(), ds) {
		t.Error("Supports() = false, want true")
	}
}

func TestSecretChecker_HealthySecret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"key": []byte("value"),
		},
	}

	c := NewSecretChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, secret)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 1 {
		t.Errorf("Check() healthy = %d, want 1", result.Healthy)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Check() issues = %d, want 0", len(result.Issues))
	}
}

func TestSecretChecker_EmptySecret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "empty-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{},
	}

	c := NewSecretChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, secret)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "EmptySecret" && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected EmptySecret issue")
	}
}

func TestSecretChecker_ValidTLSCert(t *testing.T) {
	notBefore := time.Now().Add(-24 * time.Hour)
	notAfter := time.Now().AddDate(0, 0, 90)
	certPEM, err := generateTestCert(notBefore, notAfter)
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valid-tls",
			Namespace: "default",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": []byte("fake-key"),
		},
	}

	c := NewSecretChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, secret)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 1 {
		t.Errorf("Check() healthy = %d, want 1", result.Healthy)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Check() issues = %d, want 0 (cert is valid for 90 days)", len(result.Issues))
	}
}

func TestSecretChecker_ExpiredCert(t *testing.T) {
	notBefore := time.Now().AddDate(0, 0, -60)
	notAfter := time.Now().AddDate(0, 0, -1)
	certPEM, err := generateTestCert(notBefore, notAfter)
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "expired-tls",
			Namespace: "default",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": []byte("fake-key"),
		},
	}

	c := NewSecretChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, secret)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "CertExpired" && issue.Severity == checker.SeverityCritical {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected CertExpired critical issue")
	}
}

func TestSecretChecker_CertExpiringSoon(t *testing.T) {
	notBefore := time.Now().Add(-24 * time.Hour)
	notAfter := time.Now().AddDate(0, 0, 10)
	certPEM, err := generateTestCert(notBefore, notAfter)
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "expiring-soon-tls",
			Namespace: "default",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": []byte("fake-key"),
		},
	}

	c := NewSecretChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, secret)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "CertExpiringSoon" && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected CertExpiringSoon warning issue")
	}
}

func TestSecretChecker_CertNotYetValid(t *testing.T) {
	notBefore := time.Now().AddDate(0, 0, 7)
	notAfter := time.Now().AddDate(1, 0, 0)
	certPEM, err := generateTestCert(notBefore, notAfter)
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "future-tls",
			Namespace: "default",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": []byte("fake-key"),
		},
	}

	c := NewSecretChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, secret)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "CertNotYetValid" && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected CertNotYetValid warning issue")
	}
}

func TestSecretChecker_InvalidCertPEM(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-tls",
			Namespace: "default",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": []byte("not-a-valid-cert"),
			"tls.key": []byte("fake-key"),
		},
	}

	c := NewSecretChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, secret)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "InvalidCertificate" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected InvalidCertificate issue")
	}
}

func TestSecretChecker_WithCertExpiryWarningDays(t *testing.T) {
	c := NewSecretChecker().WithCertExpiryWarningDays(14)
	if c.certExpiryWarningDays != 14 {
		t.Errorf("certExpiryWarningDays = %d, want 14", c.certExpiryWarningDays)
	}
}

func TestSecretChecker_SkipsServiceAccountTokens(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sa-token",
			Namespace: "default",
		},
		Type: corev1.SecretTypeServiceAccountToken,
		Data: map[string][]byte{},
	}

	c := NewSecretChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, secret)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	for _, issue := range result.Issues {
		if issue.Type == "EmptySecret" {
			t.Error("Check() should not report EmptySecret for service account tokens")
		}
	}
}
