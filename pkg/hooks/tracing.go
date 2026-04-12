package hooks

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/observe"
)

// TracingDocEventDispatcher wraps a DocEventDispatcher with OpenTelemetry
// spans. Each Dispatch call creates a "hooks.dispatch" span annotated with
// the doctype and event name.
type TracingDocEventDispatcher struct {
	inner *DocEventDispatcher
}

// NewTracingDocEventDispatcher wraps the given dispatcher with tracing.
func NewTracingDocEventDispatcher(inner *DocEventDispatcher) *TracingDocEventDispatcher {
	return &TracingDocEventDispatcher{inner: inner}
}

// Dispatch creates a tracing span around the inner dispatcher's Dispatch call.
// The DocContext embeds context.Context directly, so we extract the parent
// context, start a span, and build a new DocContext with the traced context.
func (d *TracingDocEventDispatcher) Dispatch(ctx *document.DocContext, doc document.Document, doctype string, event document.DocEvent) error {
	tracer := observe.Tracer("moca.hooks")

	// DocContext embeds context.Context — use it as the parent span context.
	spanCtx, span := tracer.Start(ctx.Context, "hooks.dispatch",
		trace.WithAttributes(
			attribute.String("moca.doctype", doctype),
			attribute.String("moca.event", string(event)),
		))
	defer span.End()

	// Build a new DocContext that carries the traced context while preserving
	// all other request-scoped fields (TX, Site, User, Flags, etc.).
	tracedCtx := &document.DocContext{
		Context:   spanCtx,
		TX:        ctx.TX,
		Flags:     ctx.Flags,
		RequestID: ctx.RequestID,
		Site:      ctx.Site,
		User:      ctx.User,
		EventBus:  ctx.EventBus,
	}

	err := d.inner.Dispatch(tracedCtx, doc, doctype, event)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// Ensure TracingDocEventDispatcher satisfies the HookDispatcher interface.
var _ document.HookDispatcher = (*TracingDocEventDispatcher)(nil)
