package auth

import (
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"

	"github.com/crewjam/saml"
)

// SAMLProvider implements SAML 2.0 Service Provider with POST binding.
// It wraps github.com/crewjam/saml.ServiceProvider for metadata generation,
// SSO URL construction, and assertion parsing/verification.
type SAMLProvider struct {
	cfg *SSOProviderConfig
	sp  saml.ServiceProvider
}

// NewSAMLProvider creates a SAMLProvider from the given configuration.
// metadataURL is the public URL for the SP metadata endpoint
// (e.g., "https://app.example.com/api/v1/auth/saml/metadata?provider=okta").
// acsURL is the Assertion Consumer Service URL
// (e.g., "https://app.example.com/api/v1/auth/saml/acs?provider=okta").
func NewSAMLProvider(cfg *SSOProviderConfig, metadataURL, acsURL string) (*SAMLProvider, error) {
	metaURL, err := url.Parse(metadataURL)
	if err != nil {
		return nil, fmt.Errorf("saml: parse metadata URL: %w", err)
	}
	acsU, err := url.Parse(acsURL)
	if err != nil {
		return nil, fmt.Errorf("saml: parse ACS URL: %w", err)
	}

	sp := saml.ServiceProvider{
		EntityID:          metadataURL,
		MetadataURL:       *metaURL,
		AcsURL:            *acsU,
		AllowIDPInitiated: true,
	}

	// Parse IdP certificate for signature verification.
	if cfg.IdPCertificate != "" {
		cert, err := parsePEMCertificate(cfg.IdPCertificate)
		if err != nil {
			return nil, fmt.Errorf("saml: parse IdP certificate: %w", err)
		}
		idpSSOURL, err := url.Parse(cfg.IdPSSOURL)
		if err != nil {
			return nil, fmt.Errorf("saml: parse IdP SSO URL: %w", err)
		}

		sp.IDPMetadata = &saml.EntityDescriptor{
			EntityID: cfg.IdPEntityID,
			IDPSSODescriptors: []saml.IDPSSODescriptor{
				{
					SSODescriptor: saml.SSODescriptor{
						RoleDescriptor: saml.RoleDescriptor{
							KeyDescriptors: []saml.KeyDescriptor{
								{
									Use: "signing",
									KeyInfo: saml.KeyInfo{
										X509Data: saml.X509Data{
											X509Certificates: []saml.X509Certificate{
												{Data: certToPEMData(cert)},
											},
										},
									},
								},
							},
						},
					},
					SingleSignOnServices: []saml.Endpoint{
						{
							Binding:  saml.HTTPRedirectBinding,
							Location: idpSSOURL.String(),
						},
						{
							Binding:  saml.HTTPPostBinding,
							Location: idpSSOURL.String(),
						},
					},
				},
			},
		}
	}

	// Parse SP certificate and private key if provided (for signed requests).
	if cfg.SPCertificate != "" && cfg.SPPrivateKey != "" {
		tlsCert, err := tls.X509KeyPair([]byte(cfg.SPCertificate), []byte(cfg.SPPrivateKey))
		if err != nil {
			return nil, fmt.Errorf("saml: parse SP key pair: %w", err)
		}
		rsaKey, ok := tlsCert.PrivateKey.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("saml: SP private key is not RSA")
		}
		sp.Key = rsaKey
		sp.Certificate, err = x509.ParseCertificate(tlsCert.Certificate[0])
		if err != nil {
			return nil, fmt.Errorf("saml: parse SP certificate: %w", err)
		}
	}

	return &SAMLProvider{cfg: cfg, sp: sp}, nil
}

// Metadata returns the SP metadata XML document. This should be served at the
// SP metadata endpoint for IdP configuration.
func (p *SAMLProvider) Metadata() ([]byte, error) {
	md := p.sp.Metadata()
	data, err := xml.MarshalIndent(md, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("saml: marshal metadata: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}

// AuthURL returns the IdP SSO URL with a SAMLRequest for SP-initiated SSO.
// The relayState parameter is used as the SAML RelayState, carrying the CSRF
// state token.
func (p *SAMLProvider) AuthURL(relayState string) (string, error) {
	authnReq, err := p.sp.MakeAuthenticationRequest(
		p.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding,
		saml.HTTPPostBinding,
	)
	if err != nil {
		return "", fmt.Errorf("saml: build authn request: %w", err)
	}

	redirectURL, err := authnReq.Redirect(relayState, &p.sp)
	if err != nil {
		return "", fmt.Errorf("saml: build redirect URL: %w", err)
	}
	return redirectURL.String(), nil
}

// ParseResponse processes a SAML Response POST to the ACS endpoint.
// It validates the assertion signature using the IdP certificate and extracts
// user attributes.
func (p *SAMLProvider) ParseResponse(r *http.Request) (*SSOResult, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("saml: parse form: %w", err)
	}

	assertion, err := p.sp.ParseResponse(r, []string{p.sp.EntityID})
	if err != nil {
		return nil, fmt.Errorf("saml: parse response: %w", err)
	}

	return p.extractAssertion(assertion)
}

// extractAssertion maps SAML assertion attributes to an SSOResult.
func (p *SAMLProvider) extractAssertion(assertion *saml.Assertion) (*SSOResult, error) {
	emailClaim := p.cfg.EmailClaim
	if emailClaim == "" {
		emailClaim = "email"
	}
	fullNameClaim := p.cfg.FullNameClaim
	if fullNameClaim == "" {
		fullNameClaim = "name"
	}

	attrs := make(map[string]string)
	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			if len(attr.Values) > 0 {
				attrs[attr.Name] = attr.Values[0].Value
				// Also map FriendlyName if present.
				if attr.FriendlyName != "" {
					attrs[attr.FriendlyName] = attr.Values[0].Value
				}
			}
		}
	}

	// Try NameID as email fallback.
	email := attrs[emailClaim]
	if email == "" && assertion.Subject != nil && assertion.Subject.NameID != nil {
		email = assertion.Subject.NameID.Value
	}
	if email == "" {
		return nil, ErrSSOEmailMissing
	}

	fullName := attrs[fullNameClaim]

	return &SSOResult{
		Email:         email,
		FullName:      fullName,
		RawAttributes: attrs,
	}, nil
}

// parsePEMCertificate parses a PEM-encoded X.509 certificate.
func parsePEMCertificate(pemData string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in certificate data")
	}
	return x509.ParseCertificate(block.Bytes)
}

// certToPEMData extracts the base64-encoded DER data from an x509.Certificate
// for inclusion in SAML XML metadata (without PEM headers/footers).
func certToPEMData(cert *x509.Certificate) string {
	return base64.StdEncoding.EncodeToString(cert.Raw)
}
