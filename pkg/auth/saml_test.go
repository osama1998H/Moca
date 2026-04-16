package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/crewjam/saml"
)

// generateTestCertAndKey generates a self-signed X.509 certificate and RSA
// private key for SAML testing.
func generateTestCertAndKey(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-saml-cert"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	return certPEM, keyPEM
}

func TestNewSAMLProvider_ValidConfig(t *testing.T) {
	certPEM, keyPEM := generateTestCertAndKey(t)

	cfg := &SSOProviderConfig{
		ProviderType:   "SAML",
		IdPEntityID:    "https://idp.example.com",
		IdPSSOURL:      "https://idp.example.com/sso",
		IdPCertificate: certPEM,
		SPCertificate:  certPEM,
		SPPrivateKey:   keyPEM,
		EmailClaim:     "email",
	}

	sp, err := NewSAMLProvider(cfg,
		"https://app.example.com/api/v1/auth/saml/metadata?provider=test",
		"https://app.example.com/api/v1/auth/saml/acs?provider=test",
	)
	if err != nil {
		t.Fatalf("NewSAMLProvider failed: %v", err)
	}
	if sp == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewSAMLProvider_InvalidCert(t *testing.T) {
	cfg := &SSOProviderConfig{
		IdPCertificate: "not a PEM cert",
		IdPSSOURL:      "https://idp.example.com/sso",
		IdPEntityID:    "https://idp.example.com",
	}

	_, err := NewSAMLProvider(cfg,
		"https://app.example.com/metadata",
		"https://app.example.com/acs",
	)
	if err == nil {
		t.Fatal("expected error for invalid certificate")
	}
}

func TestSAMLProvider_Metadata(t *testing.T) {
	certPEM, keyPEM := generateTestCertAndKey(t)

	cfg := &SSOProviderConfig{
		IdPEntityID:    "https://idp.example.com",
		IdPSSOURL:      "https://idp.example.com/sso",
		IdPCertificate: certPEM,
		SPCertificate:  certPEM,
		SPPrivateKey:   keyPEM,
	}

	sp, err := NewSAMLProvider(cfg,
		"https://app.example.com/api/v1/auth/saml/metadata?provider=test",
		"https://app.example.com/api/v1/auth/saml/acs?provider=test",
	)
	if err != nil {
		t.Fatalf("NewSAMLProvider: %v", err)
	}

	xmlData, err := sp.Metadata()
	if err != nil {
		t.Fatalf("Metadata failed: %v", err)
	}
	if len(xmlData) == 0 {
		t.Fatal("expected non-empty metadata XML")
	}

	// Verify it contains expected elements.
	xmlStr := string(xmlData)
	if !containsStr(xmlStr, "EntityDescriptor") {
		t.Error("metadata missing EntityDescriptor")
	}
	if !containsStr(xmlStr, "AssertionConsumerService") {
		t.Error("metadata missing AssertionConsumerService")
	}
}

func TestSAMLProvider_ExtractAssertion_Success(t *testing.T) {
	cfg := &SSOProviderConfig{
		EmailClaim:    "email",
		FullNameClaim: "displayName",
	}
	sp := &SAMLProvider{cfg: cfg}

	assertion := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{
			{
				Attributes: []saml.Attribute{
					{
						Name:         "email",
						FriendlyName: "email",
						Values: []saml.AttributeValue{
							{Value: "saml-user@example.com"},
						},
					},
					{
						Name:         "displayName",
						FriendlyName: "displayName",
						Values: []saml.AttributeValue{
							{Value: "SAML User"},
						},
					},
					{
						Name: "department",
						Values: []saml.AttributeValue{
							{Value: "Engineering"},
						},
					},
				},
			},
		},
	}

	result, err := sp.extractAssertion(assertion)
	if err != nil {
		t.Fatalf("extractAssertion failed: %v", err)
	}
	if result.Email != "saml-user@example.com" {
		t.Errorf("Email = %q, want %q", result.Email, "saml-user@example.com")
	}
	if result.FullName != "SAML User" {
		t.Errorf("FullName = %q, want %q", result.FullName, "SAML User")
	}
	if result.RawAttributes["department"] != "Engineering" {
		t.Errorf("RawAttributes[department] = %q", result.RawAttributes["department"])
	}
}

func TestSAMLProvider_ExtractAssertion_NameIDFallback(t *testing.T) {
	cfg := &SSOProviderConfig{EmailClaim: "email"}
	sp := &SAMLProvider{cfg: cfg}

	assertion := &saml.Assertion{
		Subject: &saml.Subject{
			NameID: &saml.NameID{
				Value: "nameid@example.com",
			},
		},
		AttributeStatements: []saml.AttributeStatement{
			{
				Attributes: []saml.Attribute{
					{
						Name:   "displayName",
						Values: []saml.AttributeValue{{Value: "NameID User"}},
					},
				},
			},
		},
	}

	result, err := sp.extractAssertion(assertion)
	if err != nil {
		t.Fatalf("extractAssertion failed: %v", err)
	}
	if result.Email != "nameid@example.com" {
		t.Errorf("Email = %q, want %q (NameID fallback)", result.Email, "nameid@example.com")
	}
}

func TestSAMLProvider_ExtractAssertion_MissingEmail(t *testing.T) {
	cfg := &SSOProviderConfig{EmailClaim: "email"}
	sp := &SAMLProvider{cfg: cfg}

	assertion := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{
			{
				Attributes: []saml.Attribute{
					{Name: "name", Values: []saml.AttributeValue{{Value: "Test"}}},
				},
			},
		},
	}

	_, err := sp.extractAssertion(assertion)
	if err != ErrSSOEmailMissing {
		t.Fatalf("expected ErrSSOEmailMissing, got %v", err)
	}
}

func TestParsePEMCertificate(t *testing.T) {
	certPEM, _ := generateTestCertAndKey(t)

	cert, err := parsePEMCertificate(certPEM)
	if err != nil {
		t.Fatalf("parsePEMCertificate failed: %v", err)
	}
	if cert.Subject.CommonName != "test-saml-cert" {
		t.Errorf("CommonName = %q", cert.Subject.CommonName)
	}
}

func TestParsePEMCertificate_Invalid(t *testing.T) {
	_, err := parsePEMCertificate("not a certificate")
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestSAMLProvider_ParseResponse_UsesEntityIDAudience(t *testing.T) {
	certPEM, keyPEM := generateTestCertAndKey(t)
	cfg := &SSOProviderConfig{
		IdPEntityID:    "https://idp.example.com",
		IdPSSOURL:      "https://idp.example.com/sso",
		IdPCertificate: certPEM,
		SPCertificate:  certPEM,
		SPPrivateKey:   keyPEM,
	}
	metadataURL := "https://app.example.com/api/v1/auth/saml/metadata?provider=test"
	sp, err := NewSAMLProvider(cfg, metadataURL,
		"https://app.example.com/api/v1/auth/saml/acs?provider=test")
	if err != nil {
		t.Fatalf("NewSAMLProvider: %v", err)
	}
	if sp.sp.EntityID != metadataURL {
		t.Errorf("EntityID = %q, want %q", sp.sp.EntityID, metadataURL)
	}
}

// containsStr is a simple string contains helper for tests.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
