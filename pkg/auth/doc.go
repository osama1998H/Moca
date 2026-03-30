// Package auth implements authentication and authorization for MOCA.
//
// MOCA supports multiple authentication mechanisms: session cookies,
// JWT bearer tokens, OAuth2, and SSO via SAML/OIDC. Authorization is
// permission-based with role-level and document-level access control.
//
// Key components:
//   - Session: cookie-based session management with Redis backing
//   - JWT: token issuance, validation, and refresh
//   - OAuth2: provider integration (Google, GitHub, etc.)
//   - SSO: SAML 2.0 and OIDC provider support
//   - Permission: role resolution and document-level permission checks
package auth
