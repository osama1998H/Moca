# Security Fix Report

- Selected issue: `SEC-002` JWT Tokens in Browser localStorage
- Severity: High
- Root cause: The repository already had a hardened `moca_sid` HttpOnly session cookie, but `pkg/api/auth_handler.go` still returned refresh tokens in JSON for both login and refresh flows. That exposed the long-lived credential to any Desk-side JavaScript and pushed browser clients toward JS-readable token persistence instead of the existing cookie-backed session boundary.
- Files changed:
  - `pkg/api/auth_handler.go`
  - `pkg/api/auth_handler_test.go`
- Tests run:
  - `GOCACHE=/tmp/moca-go-cache go test ./pkg/api -run 'TestRefreshTokenFromRequest|TestResponseTokenPair|TestAuthCookie' -count=1`
  - `GOCACHE=/tmp/moca-go-cache go test ./pkg/api -run '^$' -count=1`
  - `GOCACHE=/tmp/moca-go-cache go vet ./pkg/api`
  - Attempted but blocked by sandbox: `GOCACHE=/tmp/moca-go-cache go test ./pkg/api -run 'Test(Login|Logout|Refresh)' -count=1` (`miniredis` could not bind `127.0.0.1:0`)
- PR URL: Not created. Publishing was blocked because outbound `git push` could not resolve `github.com`, the GitHub connector's tree-write step was safety-gated, and `gh auth status` reported an invalid token.
- Issue status update: No open GitHub issue labeled or tracked this finding in `osama1998H/Moca`, so no issue comment or resolution update could be posted. A local fix was implemented and partially validated, but remote publication and tracker updates remain blocked by missing issue context plus GitHub write/network restrictions.
