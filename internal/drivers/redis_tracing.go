package drivers

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/osama1998H/moca/pkg/observe"
)

// RedisTracingHook implements redis.Hook to create OpenTelemetry spans for
// Redis commands and pipeline operations. Attach it via client.AddHook().
type RedisTracingHook struct{}

// DialHook passes through without tracing — connection establishment is not
// interesting enough to warrant its own span in normal operation.
func (h RedisTracingHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

// ProcessHook wraps individual Redis commands with an OpenTelemetry span.
// redis.Nil errors (cache misses) are not recorded as span errors.
func (h RedisTracingHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		tracer := observe.Tracer("moca.redis")
		ctx, span := tracer.Start(ctx, "redis."+cmd.Name(),
			trace.WithAttributes(
				attribute.String("db.system", "redis"),
				attribute.String("db.operation", cmd.Name()),
			))
		defer span.End()

		err := next(ctx, cmd)
		if err != nil && !errors.Is(err, redis.Nil) {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return err
	}
}

// ProcessPipelineHook wraps Redis pipeline executions with a single span.
// The pipeline size is recorded as an attribute.
func (h RedisTracingHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		tracer := observe.Tracer("moca.redis")
		ctx, span := tracer.Start(ctx, "redis.pipeline",
			trace.WithAttributes(
				attribute.String("db.system", "redis"),
				attribute.Int("db.pipeline.size", len(cmds)),
			))
		defer span.End()

		err := next(ctx, cmds)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return err
	}
}
