package orm

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/osama1998H/moca/pkg/observe"
)

// maxSQLLen is the maximum number of characters retained in the db.statement
// span attribute. Longer statements are truncated and suffixed with "...".
const maxSQLLen = 100

// OTelQueryTracer implements pgx.QueryTracer to record OpenTelemetry spans for
// every database query executed through pgxpool connections.
type OTelQueryTracer struct{}

// TraceQueryStart creates a new span for the query being executed. The span
// context is stored in the returned context so TraceQueryEnd can close it.
func (t *OTelQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	tracer := observe.Tracer("moca.db")
	sql := sanitizeSQL(data.SQL)
	ctx, _ = tracer.Start(ctx, "db.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.statement", sql),
		))
	return ctx
}

// TraceQueryEnd closes the span opened by TraceQueryStart. If the query
// returned an error, it is recorded on the span and the status is set to Error.
func (t *OTelQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}
	span.End()
}

// sanitizeSQL truncates the SQL statement to maxSQLLen characters and replaces
// newlines with spaces for cleaner display in trace UIs.
func sanitizeSQL(sql string) string {
	sql = strings.ReplaceAll(sql, "\n", " ")
	sql = strings.ReplaceAll(sql, "\r", " ")
	sql = strings.ReplaceAll(sql, "\t", " ")
	if len(sql) > maxSQLLen {
		return sql[:maxSQLLen] + "..."
	}
	return sql
}
