package document

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/events"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// DocContext carries request-scoped data through the entire document lifecycle.
// It is passed to every lifecycle hook, controller method, validator, and CRUD
// operation so that they all share the same user identity, transaction, and flags.
//
// DocContext embeds context.Context so it can be passed directly to any function
// that accepts a standard context (pgx queries, outbox writes, etc.).
//
//nolint:govet // Field order favors readability for a request-scoped carrier type.
type DocContext struct {
	context.Context

	// TX is the active database transaction for this request.
	// Lifecycle hooks and CRUD operations must use this transaction to ensure
	// atomicity. May be nil outside of a transaction boundary.
	TX pgx.Tx

	// Flags holds per-request control flags, such as:
	//   "skip_validation" -> true  (bypass field validation)
	//   "silent"          -> true  (suppress after-save events)
	Flags map[string]any

	// RequestID carries the HTTP/request correlation ID into audit and outbox
	// records when the document lifecycle is triggered by the API layer.
	RequestID string

	// Site identifies the current tenant and provides its connection pool.
	Site *tenancy.SiteContext

	// User is the authenticated user making the request.
	User *auth.User

	// EventBus is used to publish domain events during the lifecycle.
	// Points to the no-op Emitter until MS-15 provides a real implementation.
	EventBus *events.Emitter
}

// NewDocContext creates a DocContext wrapping the given standard context, site,
// and user. The Flags map is initialized empty and ready for use.
func NewDocContext(ctx context.Context, site *tenancy.SiteContext, user *auth.User) *DocContext {
	return &DocContext{
		Context:  ctx,
		Site:     site,
		User:     user,
		Flags:    make(map[string]any),
		EventBus: &events.Emitter{},
	}
}
