# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x-mvp | Yes |

As Moca is pre-1.0 and in active development, only the latest release receives security updates.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, report them privately via one of the following channels:

1. **GitHub Security Advisories** (preferred): Use the [Report a vulnerability](https://github.com/osama1998H/Moca/security/advisories/new) button in the Security tab of this repository.
2. **Email**: Send details to the repository maintainer directly via the email listed on the [GitHub profile](https://github.com/osama1998H).

### What to Include

- Description of the vulnerability
- Steps to reproduce (or a proof-of-concept)
- Affected component (e.g., `pkg/api`, `pkg/orm`, `pkg/auth`, CLI)
- Impact assessment (data exposure, privilege escalation, denial of service, etc.)
- Suggested fix, if you have one

### Response Timeline

- **Acknowledgment**: Within 48 hours of report
- **Initial assessment**: Within 5 business days
- **Fix or mitigation**: Depends on severity (see below)

### Severity and Response

| Severity | Example | Target Resolution |
|----------|---------|-------------------|
| Critical | SQL injection, tenant data cross-contamination, auth bypass | 48 hours |
| High | Privilege escalation, credential exposure in logs | 5 business days |
| Medium | Rate limiter bypass, information disclosure via error messages | 2 weeks |
| Low | Missing security headers, verbose error output | Next release |

## Security Architecture

Moca's design incorporates several security boundaries. When reporting vulnerabilities, consider these trust boundaries:

### Tenant Isolation
- Each tenant has a dedicated PostgreSQL schema (`tenant_{name}`)
- Connection pools are per-tenant with `search_path` set via `AfterConnect` callback
- Redis keys are prefixed by site name
- **Any cross-tenant data leakage is Critical severity**

### API Layer
- All SQL queries are parameterized (no string interpolation)
- Field names in queries are validated against MetaType definitions
- `ReportDef` execution rejects DDL keywords (DROP, ALTER, TRUNCATE, etc.)
- Rate limiting via Redis sliding window (per-user, per-tenant)
- Auth is currently a placeholder (`NoopAuthenticator`) — production auth planned for MS-14

### Document Runtime
- Field-level validation enforces types, lengths, regex patterns
- Read-only fields are enforced at the API transformer layer
- Audit log records all mutations to `tab_audit_log`

### CLI
- Passwords are read via terminal (not echoed)
- PID files use restrictive permissions
- Config files may contain credentials — `.gitignore` should exclude site-specific configs

## Known Limitations (Pre-1.0)

The following security features are **not yet implemented** and are planned for future milestones:

| Feature | Planned Milestone |
|---------|-------------------|
| JWT / Session / OAuth2 authentication | MS-14 |
| Role-based permission engine (RBAC) | MS-14 |
| Row-level security (RLS) policies | MS-14 |
| API key management | MS-18 |
| CSRF protection | MS-22 |
| Content Security Policy headers | MS-22 |
| TLS configuration | MS-21 |
| Secret encryption at rest | MS-22 |
| Audit log tamper protection | MS-22 |

**Moca v0.1.0-mvp is not intended for production use with untrusted users.** The auth layer is a placeholder. Deploy behind a trusted network or VPN until MS-14 (Permission Engine) and MS-22 (Security Hardening) are complete.

## Disclosure Policy

- We follow coordinated disclosure. We ask reporters to give us reasonable time to fix issues before public disclosure.
- Credit will be given to reporters in the release notes (unless they prefer to remain anonymous).
- We will not pursue legal action against researchers acting in good faith.
