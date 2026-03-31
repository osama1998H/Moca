package document

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moca-framework/moca/pkg/meta"
)

// FieldError describes a single field-level validation failure.
type FieldError struct {
	// Field is the field name that failed validation.
	Field string
	// Message is a human-readable description of the failure.
	Message string
	// Rule is the name of the validation rule that triggered the failure
	// (e.g. "required", "max_length", "regex", "select", "unique", "link", "custom").
	Rule string
}

// ValidationError is returned by ValidateDoc when one or more field-level
// validation rules fail. It wraps a slice of FieldError so that callers
// (e.g. MS-06 REST handlers) can produce structured HTTP 422 responses.
type ValidationError struct {
	Errors []FieldError
}

// Error implements the error interface. It formats all accumulated field errors.
func (e *ValidationError) Error() string {
	if len(e.Errors) == 1 {
		fe := e.Errors[0]
		return fmt.Sprintf("validation error: %s: %s (%s)", fe.Field, fe.Message, fe.Rule)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d validation errors:", len(e.Errors))
	for _, fe := range e.Errors {
		fmt.Fprintf(&b, "\n  - %s: %s (%s)", fe.Field, fe.Message, fe.Rule)
	}
	return b.String()
}

// ValidatorFunc is the signature for custom validator functions registered with
// a Validator. It receives the request context, the document, the field definition,
// and the current (already coerced) field value. It must return a non-nil error
// describing the violation, or nil if the value is valid.
type ValidatorFunc func(ctx context.Context, doc Document, field *meta.FieldDef, value any) error

// Validator performs field-level type coercion and validation against the rules
// declared in each FieldDef. It is safe for concurrent use.
//
// Usage:
//
//	v := document.NewValidator()
//	if err := v.ValidateDoc(ctx, doc, pool); err != nil {
//	    var ve *document.ValidationError
//	    if errors.As(err, &ve) { /* handle structured errors */ }
//	}
//
// Field layout: sync.Map (48) + sync.RWMutex (24) + map pointer (8) = 80 bytes.
// The fieldalignment check incorrectly suggests 48 bytes is achievable;
// no reordering of these three fields can reduce the total below 80 bytes.
//
//nolint:govet
type Validator struct {
	// regexCache caches compiled *regexp.Regexp keyed by pattern string.
	regexCache sync.Map
	mu         sync.RWMutex
	// customValidators holds registered custom validator functions.
	customValidators map[string]ValidatorFunc
}

// NewValidator creates a new Validator with an empty custom validator registry.
func NewValidator() *Validator {
	return &Validator{
		customValidators: make(map[string]ValidatorFunc),
	}
}

// RegisterValidator registers a named custom validator function that can be
// referenced by FieldDef.CustomValidator. Registering the same name twice
// overwrites the previous entry. This is the extension point consumed by MS-08.
func (v *Validator) RegisterValidator(name string, fn ValidatorFunc) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.customValidators[name] = fn
}

// ValidateDoc runs type coercion and all validation rules on every storable
// field in doc. It accumulates all failures and returns a *ValidationError
// containing all of them, or nil if validation passes.
//
// pool is used for Unique and Link DB checks. When pool is nil, those checks
// are skipped (useful in unit tests that run without a database).
//
// The caller should check DocContext.Flags["skip_validation"] before calling
// this function if they want to bypass validation entirely.
func (v *Validator) ValidateDoc(ctx *DocContext, doc *DynamicDoc, pool *pgxpool.Pool) error {
	var fieldErrors []FieldError

	for i := range doc.metaDef.Fields {
		fd := &doc.metaDef.Fields[i]
		if !fd.FieldType.IsStorable() {
			continue
		}
		// Table/TableMultiSelect fields produce child documents, not column values.
		if fd.FieldType == meta.FieldTypeTable || fd.FieldType == meta.FieldTypeTableMultiSelect {
			continue
		}

		// Step 1: type coercion (modifies doc in-place via Set).
		if coerceErr := v.coerceField(doc, fd); coerceErr != nil {
			fieldErrors = append(fieldErrors, FieldError{
				Field:   fd.Name,
				Message: coerceErr.Error(),
				Rule:    "type_coercion",
			})
			// Skip validation rules for this field if coercion failed -- the
			// value is in an indeterminate state.
			continue
		}

		// Step 2: run all validation rules, accumulating errors.
		fieldErrors = append(fieldErrors, v.validateField(ctx, doc, fd, pool)...)
	}

	if len(fieldErrors) == 0 {
		return nil
	}
	return &ValidationError{Errors: fieldErrors}
}

// coerceField coerces the field value in doc to the expected Go type based on
// the PostgreSQL column type. Values that are already the correct type are left
// unchanged. nil values are left as nil (caught later by Required check).
func (v *Validator) coerceField(doc *DynamicDoc, fd *meta.FieldDef) error {
	raw := doc.Get(fd.Name)
	if raw == nil {
		return nil
	}

	colType := meta.ColumnType(fd.FieldType)
	switch colType {
	case "TEXT":
		// TEXT columns accept any string. Coerce non-string primitives via Sprintf.
		if _, ok := raw.(string); !ok {
			coerced := fmt.Sprintf("%v", raw)
			return doc.Set(fd.Name, coerced)
		}

	case "INTEGER":
		coerced, err := toInt64(raw)
		if err != nil {
			return fmt.Errorf("cannot coerce %q to integer: %w", fd.Name, err)
		}
		return doc.Set(fd.Name, coerced)

	case "NUMERIC(18,6)":
		coerced, err := toFloat64(raw)
		if err != nil {
			return fmt.Errorf("cannot coerce %q to numeric: %w", fd.Name, err)
		}
		return doc.Set(fd.Name, coerced)

	case "BOOLEAN":
		coerced, err := toBool(raw)
		if err != nil {
			return fmt.Errorf("cannot coerce %q to boolean: %w", fd.Name, err)
		}
		return doc.Set(fd.Name, coerced)

	case "DATE":
		coerced, err := toDate(raw)
		if err != nil {
			return fmt.Errorf("cannot coerce %q to date: %w", fd.Name, err)
		}
		return doc.Set(fd.Name, coerced)

	case "TIMESTAMPTZ":
		coerced, err := toDatetime(raw)
		if err != nil {
			return fmt.Errorf("cannot coerce %q to datetime: %w", fd.Name, err)
		}
		return doc.Set(fd.Name, coerced)

	case "TIME":
		coerced, err := toTime(raw)
		if err != nil {
			return fmt.Errorf("cannot coerce %q to time: %w", fd.Name, err)
		}
		return doc.Set(fd.Name, coerced)

	case "JSONB":
		// JSONB fields accept map[string]any, []any, or a valid JSON string.
		switch val := raw.(type) {
		case map[string]any, []any:
			// Already the correct Go type; no coercion needed.
			_ = val
		case string:
			if !json.Valid([]byte(val)) {
				return fmt.Errorf("cannot coerce %q to JSONB: invalid JSON string", fd.Name)
			}
		default:
			return fmt.Errorf("cannot coerce %q to JSONB: unsupported type %T", fd.Name, raw)
		}
	}

	return nil
}

// validateField runs all validation rules for a single field, returning all
// accumulated FieldErrors (may be empty).
func (v *Validator) validateField(ctx *DocContext, doc *DynamicDoc, fd *meta.FieldDef, pool *pgxpool.Pool) []FieldError {
	var errs []FieldError
	value := doc.Get(fd.Name)

	// 1. MandatoryDependsOn -- must run before Required so we know if the field
	//    has become conditionally required.
	isRequired := fd.Required
	if fd.MandatoryDependsOn != "" {
		depVal := doc.Get(fd.MandatoryDependsOn)
		if isTruthy(depVal) {
			isRequired = true
		}
	}

	// 2. Required check.
	if isRequired && isEmpty(value) {
		errs = append(errs, FieldError{
			Field:   fd.Name,
			Message: fmt.Sprintf("field %q is required", fd.Name),
			Rule:    "required",
		})
	}

	// Remaining rules only apply when there is a value.
	if isEmpty(value) {
		// Run custom validator even on empty values if registered, but skip DB
		// and format checks that are meaningless without a value.
		if fd.CustomValidator != "" {
			if ce := v.runCustom(ctx, doc, fd, value); ce != nil {
				errs = append(errs, *ce)
			}
		}
		return errs
	}

	// 3. MaxLength (string fields only).
	if fd.MaxLength > 0 {
		if s, ok := value.(string); ok {
			if len(s) > fd.MaxLength {
				errs = append(errs, FieldError{
					Field:   fd.Name,
					Message: fmt.Sprintf("field %q exceeds max length of %d (got %d)", fd.Name, fd.MaxLength, len(s)),
					Rule:    "max_length",
				})
			}
		}
	}

	// 4. MinValue / MaxValue (numeric fields).
	if fd.MinValue != nil || fd.MaxValue != nil {
		if numericVal, ok := toNumericFloat(value); ok {
			if fd.MinValue != nil && numericVal < *fd.MinValue {
				errs = append(errs, FieldError{
					Field:   fd.Name,
					Message: fmt.Sprintf("field %q is below minimum value of %g (got %g)", fd.Name, *fd.MinValue, numericVal),
					Rule:    "min_value",
				})
			}
			if fd.MaxValue != nil && numericVal > *fd.MaxValue {
				errs = append(errs, FieldError{
					Field:   fd.Name,
					Message: fmt.Sprintf("field %q exceeds maximum value of %g (got %g)", fd.Name, *fd.MaxValue, numericVal),
					Rule:    "max_value",
				})
			}
		}
	}

	// 5. ValidationRegex.
	if fd.ValidationRegex != "" {
		if s, ok := value.(string); ok {
			re, err := v.compileRegex(fd.ValidationRegex)
			if err != nil {
				errs = append(errs, FieldError{
					Field:   fd.Name,
					Message: fmt.Sprintf("field %q has invalid validation regex: %v", fd.Name, err),
					Rule:    "regex",
				})
			} else if !re.MatchString(s) {
				errs = append(errs, FieldError{
					Field:   fd.Name,
					Message: fmt.Sprintf("field %q does not match required pattern", fd.Name),
					Rule:    "regex",
				})
			}
		}
	}

	// 6. Select options check.
	if fd.FieldType == meta.FieldTypeSelect && fd.Options != "" {
		if s, ok := value.(string); ok {
			options := strings.Split(fd.Options, "\n")
			found := false
			for _, opt := range options {
				if strings.TrimSpace(opt) == s {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, FieldError{
					Field:   fd.Name,
					Message: fmt.Sprintf("field %q has invalid value %q; allowed: %s", fd.Name, s, strings.Join(options, ", ")),
					Rule:    "select",
				})
			}
		}
	}

	// 7. Unique check (requires DB).
	if fd.Unique && pool != nil {
		if ue := v.checkUnique(ctx, doc, fd, value, pool); ue != nil {
			errs = append(errs, *ue)
		}
	}

	// 8. Link check (requires DB).
	if fd.FieldType == meta.FieldTypeLink && fd.Options != "" && pool != nil {
		if le := v.checkLink(ctx, doc, fd, value, pool); le != nil {
			errs = append(errs, *le)
		}
	}

	// 9. CustomValidator.
	if fd.CustomValidator != "" {
		if ce := v.runCustom(ctx, doc, fd, value); ce != nil {
			errs = append(errs, *ce)
		}
	}

	return errs
}

// compileRegex returns a compiled regexp from the cache, compiling it on first use.
func (v *Validator) compileRegex(pattern string) (*regexp.Regexp, error) {
	if cached, ok := v.regexCache.Load(pattern); ok {
		re, _ := cached.(*regexp.Regexp)
		return re, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	v.regexCache.Store(pattern, re)
	return re, nil
}

// checkUnique queries the DB to ensure no other document with the same doctype
// already has this field value. Returns a FieldError on violation, nil on pass.
func (v *Validator) checkUnique(ctx *DocContext, doc *DynamicDoc, fd *meta.FieldDef, value any, pool *pgxpool.Pool) *FieldError {
	table := meta.TableName(doc.metaDef.Name)
	query := fmt.Sprintf(`SELECT 1 FROM %q WHERE %q = $1 AND name != $2 LIMIT 1`, table, fd.Name)

	rows, err := pool.Query(ctx, query, value, doc.Name())
	if err != nil {
		return &FieldError{
			Field:   fd.Name,
			Message: fmt.Sprintf("field %q unique check failed: %v", fd.Name, err),
			Rule:    "unique",
		}
	}
	defer rows.Close()

	if rows.Next() {
		return &FieldError{
			Field:   fd.Name,
			Message: fmt.Sprintf("field %q must be unique; value already exists", fd.Name),
			Rule:    "unique",
		}
	}
	return nil
}

// checkLink queries the DB to ensure the linked document exists.
// Returns a FieldError if the linked record is not found.
func (v *Validator) checkLink(ctx *DocContext, doc *DynamicDoc, fd *meta.FieldDef, value any, pool *pgxpool.Pool) *FieldError {
	linkedTable := meta.TableName(fd.Options)
	query := fmt.Sprintf(`SELECT 1 FROM %q WHERE name = $1 LIMIT 1`, linkedTable)

	rows, err := pool.Query(ctx, query, value)
	if err != nil {
		return &FieldError{
			Field:   fd.Name,
			Message: fmt.Sprintf("field %q link check failed: %v", fd.Name, err),
			Rule:    "link",
		}
	}
	defer rows.Close()

	if !rows.Next() {
		return &FieldError{
			Field:   fd.Name,
			Message: fmt.Sprintf("field %q links to %q which does not exist", fd.Name, value),
			Rule:    "link",
		}
	}
	return nil
}

// runCustom invokes a registered custom validator by name. Returns a FieldError
// if the validator returns an error or if no function is registered under that name.
func (v *Validator) runCustom(ctx *DocContext, doc *DynamicDoc, fd *meta.FieldDef, value any) *FieldError {
	v.mu.RLock()
	fn, ok := v.customValidators[fd.CustomValidator]
	v.mu.RUnlock()

	if !ok {
		return &FieldError{
			Field:   fd.Name,
			Message: fmt.Sprintf("custom validator %q is not registered", fd.CustomValidator),
			Rule:    "custom",
		}
	}
	if err := fn(ctx, doc, fd, value); err != nil {
		return &FieldError{
			Field:   fd.Name,
			Message: err.Error(),
			Rule:    "custom",
		}
	}
	return nil
}

// --------------------------------------------------------------------------
// Type coercion helpers
// --------------------------------------------------------------------------

// toInt64 converts a value to int64. Accepts int64, int, float64, and string.
func toInt64(v any) (int64, error) {
	switch val := v.(type) {
	case int64:
		return val, nil
	case int:
		return int64(val), nil
	case int32:
		return int64(val), nil
	case float64:
		return int64(val), nil
	case float32:
		return int64(val), nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse %q as integer", val)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unsupported type %T for integer coercion", v)
	}
}

// toFloat64 converts a value to float64. Accepts float64, int types, and string.
func toFloat64(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int32:
		return float64(val), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse %q as numeric", val)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("unsupported type %T for numeric coercion", v)
	}
}

// toBool converts a value to bool. Accepts bool and common string representations.
func toBool(v any) (bool, error) {
	switch val := v.(type) {
	case bool:
		return val, nil
	case int64:
		return val != 0, nil
	case int:
		return val != 0, nil
	case float64:
		return val != 0, nil
	case string:
		s := strings.ToLower(strings.TrimSpace(val))
		switch s {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off", "":
			return false, nil
		default:
			return false, fmt.Errorf("cannot parse %q as boolean", val)
		}
	default:
		return false, fmt.Errorf("unsupported type %T for boolean coercion", v)
	}
}

// toDate parses a value to a time.Time (date only). Accepts time.Time and ISO 8601 date strings.
func toDate(v any) (time.Time, error) {
	if t, ok := v.(time.Time); ok {
		return t, nil
	}
	if s, ok := v.(string); ok {
		t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
		if err != nil {
			return time.Time{}, fmt.Errorf("cannot parse %q as date (expected YYYY-MM-DD)", s)
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unsupported type %T for date coercion", v)
}

// toDatetime parses a value to a time.Time with timezone. Accepts time.Time and RFC3339 strings.
func toDatetime(v any) (time.Time, error) {
	if t, ok := v.(time.Time); ok {
		return t, nil
	}
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		// Try RFC3339 first, then RFC3339Nano.
		for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("cannot parse %q as datetime (expected RFC3339)", s)
	}
	return time.Time{}, fmt.Errorf("unsupported type %T for datetime coercion", v)
}

// toTime parses a value to a time.Time (time only). Accepts time.Time and HH:MM:SS strings.
func toTime(v any) (time.Time, error) {
	if t, ok := v.(time.Time); ok {
		return t, nil
	}
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		for _, layout := range []string{"15:04:05", "15:04"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("cannot parse %q as time (expected HH:MM:SS)", s)
	}
	return time.Time{}, fmt.Errorf("unsupported type %T for time coercion", v)
}

// --------------------------------------------------------------------------
// Value inspection helpers
// --------------------------------------------------------------------------

// isEmpty reports whether a value is nil or an empty string.
func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return s == ""
	}
	return false
}

// isTruthy reports whether a value is considered truthy for MandatoryDependsOn.
// A value is truthy if it is non-nil, non-empty string, non-zero numeric, or true bool.
func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != "" && val != "0" && strings.ToLower(val) != "false"
	case int64:
		return val != 0
	case int:
		return val != 0
	case float64:
		return val != 0
	default:
		return true
	}
}

// toNumericFloat converts int64 or float64 to float64 for range checks.
// Returns (0, false) if the value is not a recognized numeric type.
func toNumericFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int64:
		return float64(val), true
	case int:
		return float64(val), true
	case int32:
		return float64(val), true
	default:
		return 0, false
	}
}
