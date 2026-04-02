package document_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// --------------------------------------------------------------------------
// Test fixture helpers
// --------------------------------------------------------------------------

// newValidatorCtx creates a DocContext for validator tests.
func newValidatorCtx() *document.DocContext {
	site := &tenancy.SiteContext{Name: "test-site"}
	user := &auth.User{Email: "admin@example.com", FullName: "Admin"}
	return document.NewDocContext(context.Background(), site, user)
}

// validateDoc is a convenience wrapper that calls ValidateDoc with nil pool
// (unit tests don't have a database connection).
func validateDoc(t *testing.T, v *document.Validator, ctx *document.DocContext, doc *document.DynamicDoc) error {
	t.Helper()
	return v.ValidateDoc(ctx, doc, nil)
}

// extractValidationError asserts that err is a *ValidationError and returns it.
func extractValidationError(t *testing.T, err error) *document.ValidationError {
	t.Helper()
	if err == nil {
		t.Fatal("expected a ValidationError but got nil")
	}
	var ve *document.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	return ve
}

// findFieldError returns the first FieldError for the given field name, or nil.
func findFieldError(ve *document.ValidationError, field, rule string) *document.FieldError {
	for i := range ve.Errors {
		fe := &ve.Errors[i]
		if fe.Field == field && (rule == "" || fe.Rule == rule) {
			return fe
		}
	}
	return nil
}

// --------------------------------------------------------------------------
// ValidationError formatting
// --------------------------------------------------------------------------

func TestValidationError_SingleError(t *testing.T) {
	ve := &document.ValidationError{
		Errors: []document.FieldError{
			{Field: "name", Message: "field is required", Rule: "required"},
		},
	}
	msg := ve.Error()
	if !strings.Contains(msg, "name") {
		t.Errorf("error message should contain field name; got: %q", msg)
	}
	if !strings.Contains(msg, "required") {
		t.Errorf("error message should contain rule name; got: %q", msg)
	}
	t.Logf("single-error format: %s", msg)
}

func TestValidationError_MultipleErrors(t *testing.T) {
	ve := &document.ValidationError{
		Errors: []document.FieldError{
			{Field: "customer", Message: "field is required", Rule: "required"},
			{Field: "amount", Message: "below minimum value", Rule: "min_value"},
		},
	}
	msg := ve.Error()
	if !strings.Contains(msg, "2 validation errors") {
		t.Errorf("multi-error format should mention count; got: %q", msg)
	}
	if !strings.Contains(msg, "customer") || !strings.Contains(msg, "amount") {
		t.Errorf("multi-error format should mention both fields; got: %q", msg)
	}
	t.Logf("multi-error format: %s", msg)
}

// --------------------------------------------------------------------------
// Required rule
// --------------------------------------------------------------------------

func TestValidator_Required_NilValue(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "RequiredTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "title", "field_type": "Data", "label": "Title", "required": true}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	v := document.NewValidator()
	ctx := newValidatorCtx()

	err := validateDoc(t, v, ctx, doc)
	ve := extractValidationError(t, err)

	fe := findFieldError(ve, "title", "required")
	if fe == nil {
		t.Fatalf("expected required error for 'title'; errors: %v", ve.Errors)
	}
	t.Logf("required nil: %v", fe)
}

func TestValidator_Required_EmptyString(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "RequiredTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "title", "field_type": "Data", "label": "Title", "required": true}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("title", ""))
	v := document.NewValidator()
	ctx := newValidatorCtx()

	err := validateDoc(t, v, ctx, doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "title", "required") == nil {
		t.Fatalf("expected required error for empty string; errors: %v", ve.Errors)
	}
	t.Logf("required empty string: ok")
}

func TestValidator_Required_ValuePresent(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "RequiredTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "title", "field_type": "Data", "label": "Title", "required": true}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("title", "Hello"))
	v := document.NewValidator()

	err := validateDoc(t, v, document.NewDocContext(context.Background(), nil, nil), doc)
	if err != nil {
		t.Errorf("expected no error when required field is set; got: %v", err)
	}
	t.Logf("required value present: no error")
}

// --------------------------------------------------------------------------
// MaxLength rule
// --------------------------------------------------------------------------

func TestValidator_MaxLength_Exceeded(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "MaxLenTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "code", "field_type": "Data", "label": "Code", "max_length": 5}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("code", "toolong"))
	v := document.NewValidator()
	ctx := newValidatorCtx()

	err := validateDoc(t, v, ctx, doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "code", "max_length") == nil {
		t.Fatalf("expected max_length error; errors: %v", ve.Errors)
	}
	t.Logf("max_length exceeded: ok")
}

func TestValidator_MaxLength_OK(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "MaxLenTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "code", "field_type": "Data", "label": "Code", "max_length": 10}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("code", "short"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Errorf("expected no error for value within max_length; got: %v", err)
	}
	t.Logf("max_length within limit: no error")
}

// --------------------------------------------------------------------------
// MinValue / MaxValue rules
// --------------------------------------------------------------------------

func TestValidator_MinValue_Violated(t *testing.T) {
	minVal := float64(10)
	mt := mustCompile(t, `{
		"name": "RangeTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "qty", "field_type": "Int", "label": "Qty", "min_value": 10}
		]
	}`)
	_ = minVal
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("qty", int64(5)))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "qty", "min_value") == nil {
		t.Fatalf("expected min_value error; errors: %v", ve.Errors)
	}
	t.Logf("min_value violated: ok")
}

func TestValidator_MaxValue_Violated(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "RangeTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "score", "field_type": "Float", "label": "Score", "max_value": 100}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("score", float64(150)))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "score", "max_value") == nil {
		t.Fatalf("expected max_value error; errors: %v", ve.Errors)
	}
	t.Logf("max_value violated: ok")
}

func TestValidator_MinMaxValue_OK(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "RangeTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "score", "field_type": "Float", "label": "Score", "min_value": 0, "max_value": 100}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("score", float64(50)))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Errorf("expected no error for value within range; got: %v", err)
	}
	t.Logf("min/max value within range: no error")
}

// --------------------------------------------------------------------------
// ValidationRegex rule
// --------------------------------------------------------------------------

func TestValidator_Regex_Violation(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "RegexTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "phone", "field_type": "Data", "label": "Phone", "validation_regex": "^\\+[0-9]{7,15}$"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("phone", "not-a-phone"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "phone", "regex") == nil {
		t.Fatalf("expected regex error; errors: %v", ve.Errors)
	}
	t.Logf("regex violation detected: ok")
}

func TestValidator_Regex_Pass(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "RegexTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "phone", "field_type": "Data", "label": "Phone", "validation_regex": "^\\+[0-9]{7,15}$"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("phone", "+12345678"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Errorf("expected no error for valid phone; got: %v", err)
	}
	t.Logf("regex valid value: no error")
}

func TestValidator_Regex_InvalidPattern(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "RegexTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "field1", "field_type": "Data", "label": "F1", "validation_regex": "[invalid(regex"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("field1", "somevalue"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "field1", "regex") == nil {
		t.Fatalf("expected regex compile error; errors: %v", ve.Errors)
	}
	t.Logf("invalid regex pattern reported: ok")
}

// --------------------------------------------------------------------------
// Select options rule
// --------------------------------------------------------------------------

func TestValidator_Select_InvalidOption(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "SelectTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "status", "field_type": "Select", "label": "Status", "options": "Draft\nSubmitted\nCancelled"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("status", "Invalid"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "status", "select") == nil {
		t.Fatalf("expected select error; errors: %v", ve.Errors)
	}
	t.Logf("invalid select option detected: ok")
}

func TestValidator_Select_ValidOption(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "SelectTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "status", "field_type": "Select", "label": "Status", "options": "Draft\nSubmitted\nCancelled"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("status", "Draft"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Errorf("expected no error for valid select option; got: %v", err)
	}
	t.Logf("valid select option: no error")
}

// --------------------------------------------------------------------------
// MandatoryDependsOn rule
// --------------------------------------------------------------------------

func TestValidator_MandatoryDependsOn_Triggered(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "DepTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "has_discount", "field_type": "Check", "label": "Has Discount"},
			{"name": "discount_pct", "field_type": "Float", "label": "Discount %", "mandatory_depends_on": "has_discount"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	// Set has_discount to true -- makes discount_pct required.
	assertNoError(t, doc.Set("has_discount", true))
	// Leave discount_pct unset.
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "discount_pct", "required") == nil {
		t.Fatalf("expected required error for discount_pct when has_discount=true; errors: %v", ve.Errors)
	}
	t.Logf("mandatory_depends_on triggered: ok")
}

func TestValidator_MandatoryDependsOn_NotTriggered(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "DepTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "has_discount", "field_type": "Check", "label": "Has Discount"},
			{"name": "discount_pct", "field_type": "Float", "label": "Discount %", "mandatory_depends_on": "has_discount"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("has_discount", false))
	// discount_pct left empty -- no error expected because condition is false.
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Errorf("expected no error when mandatory_depends_on field is false; got: %v", err)
	}
	t.Logf("mandatory_depends_on not triggered: no error")
}

// --------------------------------------------------------------------------
// Type coercion: Int
// --------------------------------------------------------------------------

func TestValidator_Coerce_StringToInt(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CoerceTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "qty", "field_type": "Int", "label": "Qty"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("qty", "42"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Fatalf("expected no error after string->int coercion; got: %v", err)
	}
	coerced := doc.Get("qty")
	if coerced != int64(42) {
		t.Errorf("expected qty to be int64(42) after coercion, got %T(%v)", coerced, coerced)
	}
	t.Logf("string->int coercion: qty=%v (%T)", coerced, coerced)
}

func TestValidator_Coerce_InvalidStringToInt(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CoerceTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "qty", "field_type": "Int", "label": "Qty"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("qty", "abc"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "qty", "type_coercion") == nil {
		t.Fatalf("expected type_coercion error for invalid int string; errors: %v", ve.Errors)
	}
	t.Logf("invalid int string coercion error: ok")
}

// --------------------------------------------------------------------------
// Type coercion: Float / Numeric
// --------------------------------------------------------------------------

func TestValidator_Coerce_StringToFloat(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CoerceTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "price", "field_type": "Float", "label": "Price"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("price", "99.99"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Fatalf("expected no error after string->float coercion; got: %v", err)
	}
	coerced := doc.Get("price")
	if coerced != float64(99.99) {
		t.Errorf("expected price to be float64(99.99), got %T(%v)", coerced, coerced)
	}
	t.Logf("string->float coercion: price=%v (%T)", coerced, coerced)
}

func TestValidator_Coerce_IntToFloat(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CoerceTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "price", "field_type": "Currency", "label": "Price"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("price", int64(100)))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Fatalf("expected no error after int->float coercion; got: %v", err)
	}
	coerced := doc.Get("price")
	if coerced != float64(100) {
		t.Errorf("expected price to be float64(100), got %T(%v)", coerced, coerced)
	}
	t.Logf("int->float coercion: price=%v (%T)", coerced, coerced)
}

// --------------------------------------------------------------------------
// Type coercion: Boolean (Check)
// --------------------------------------------------------------------------

func TestValidator_Coerce_StringToTrue(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CoerceTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "active", "field_type": "Check", "label": "Active"}
		]
	}`)

	cases := []struct {
		input    string
		expected bool
	}{
		{"true", true},
		{"1", true},
		{"yes", true},
		{"on", true},
		{"false", false},
		{"0", false},
		{"no", false},
		{"off", false},
		{"", false},
	}

	v := document.NewValidator()
	for _, tc := range cases {
		doc := document.NewDynamicDoc(mt, nil, true)
		assertNoError(t, doc.Set("active", tc.input))

		err := validateDoc(t, v, newValidatorCtx(), doc)
		if err != nil {
			t.Fatalf("unexpected error coercing %q to bool: %v", tc.input, err)
		}
		got := doc.Get("active")
		if got != tc.expected {
			t.Errorf("coerce %q: got %v, want %v", tc.input, got, tc.expected)
		}
	}
	t.Logf("string->bool coercion: all cases passed")
}

func TestValidator_Coerce_InvalidStringToBool(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CoerceTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "active", "field_type": "Check", "label": "Active"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("active", "maybe"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "active", "type_coercion") == nil {
		t.Fatalf("expected type_coercion error; errors: %v", ve.Errors)
	}
	t.Logf("invalid bool string coercion error: ok")
}

// --------------------------------------------------------------------------
// Type coercion: Date
// --------------------------------------------------------------------------

func TestValidator_Coerce_StringToDate(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CoerceTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "due_date", "field_type": "Date", "label": "Due Date"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("due_date", "2026-03-31"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Fatalf("expected no error for valid date string; got: %v", err)
	}
	coerced, ok := doc.Get("due_date").(time.Time)
	if !ok {
		t.Fatalf("expected time.Time after date coercion, got %T", doc.Get("due_date"))
	}
	if coerced.Year() != 2026 || coerced.Month() != 3 || coerced.Day() != 31 {
		t.Errorf("unexpected date value: %v", coerced)
	}
	t.Logf("string->date coercion: %v", coerced)
}

func TestValidator_Coerce_InvalidDate(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CoerceTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "due_date", "field_type": "Date", "label": "Due Date"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("due_date", "not-a-date"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "due_date", "type_coercion") == nil {
		t.Fatalf("expected type_coercion error for invalid date; errors: %v", ve.Errors)
	}
	t.Logf("invalid date coercion error: ok")
}

// --------------------------------------------------------------------------
// Type coercion: Datetime
// --------------------------------------------------------------------------

func TestValidator_Coerce_StringToDatetime(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CoerceTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "created_at", "field_type": "Datetime", "label": "Created At"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("created_at", "2026-03-31T12:00:00Z"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Fatalf("expected no error for valid RFC3339 datetime; got: %v", err)
	}
	_, ok := doc.Get("created_at").(time.Time)
	if !ok {
		t.Fatalf("expected time.Time after datetime coercion, got %T", doc.Get("created_at"))
	}
	t.Logf("string->datetime coercion: ok")
}

// --------------------------------------------------------------------------
// Type coercion: Time
// --------------------------------------------------------------------------

func TestValidator_Coerce_StringToTime(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CoerceTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "open_time", "field_type": "Time", "label": "Open Time"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("open_time", "09:00:00"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Fatalf("expected no error for valid time string; got: %v", err)
	}
	_, ok := doc.Get("open_time").(time.Time)
	if !ok {
		t.Fatalf("expected time.Time after time coercion, got %T", doc.Get("open_time"))
	}
	t.Logf("string->time coercion: ok")
}

// --------------------------------------------------------------------------
// Error accumulation (multiple errors, not short-circuit)
// --------------------------------------------------------------------------

func TestValidator_MultipleErrors_Accumulated(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "MultiTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "title",  "field_type": "Data",  "label": "Title",  "required": true},
			{"name": "code",   "field_type": "Data",  "label": "Code",   "max_length": 3},
			{"name": "status", "field_type": "Select","label": "Status",  "options": "Draft\nActive"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	// title: nil (required violation)
	assertNoError(t, doc.Set("code", "toolong"))   // max_length violation
	assertNoError(t, doc.Set("status", "Unknown")) // select violation

	v := document.NewValidator()
	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if len(ve.Errors) < 3 {
		t.Errorf("expected at least 3 errors (required, max_length, select); got %d: %v", len(ve.Errors), ve.Errors)
	}

	if findFieldError(ve, "title", "required") == nil {
		t.Errorf("missing required error for title")
	}
	if findFieldError(ve, "code", "max_length") == nil {
		t.Errorf("missing max_length error for code")
	}
	if findFieldError(ve, "status", "select") == nil {
		t.Errorf("missing select error for status")
	}
	t.Logf("accumulated %d errors: ok", len(ve.Errors))
}

// --------------------------------------------------------------------------
// CustomValidator
// --------------------------------------------------------------------------

func TestValidator_CustomValidator_Registered(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CustomTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "email", "field_type": "Data", "label": "Email", "custom_validator": "check_email"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("email", "not-an-email"))

	v := document.NewValidator()
	v.RegisterValidator("check_email", func(ctx context.Context, doc document.Document, field *meta.FieldDef, value any) error {
		s, _ := value.(string)
		if !strings.Contains(s, "@") {
			return fmt.Errorf("invalid email address")
		}
		return nil
	})

	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "email", "custom") == nil {
		t.Fatalf("expected custom validator error; errors: %v", ve.Errors)
	}
	t.Logf("custom validator triggered: ok")
}

func TestValidator_CustomValidator_Pass(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CustomTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "email", "field_type": "Data", "label": "Email", "custom_validator": "check_email"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("email", "user@example.com"))

	v := document.NewValidator()
	v.RegisterValidator("check_email", func(ctx context.Context, doc document.Document, field *meta.FieldDef, value any) error {
		s, _ := value.(string)
		if !strings.Contains(s, "@") {
			return fmt.Errorf("invalid email address")
		}
		return nil
	})

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Errorf("expected no error for valid email; got: %v", err)
	}
	t.Logf("custom validator pass: ok")
}

func TestValidator_CustomValidator_Unregistered(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "CustomTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "code", "field_type": "Data", "label": "Code", "custom_validator": "not_registered"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("code", "somevalue"))

	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	ve := extractValidationError(t, err)

	if findFieldError(ve, "code", "custom") == nil {
		t.Fatalf("expected custom error for unregistered validator; errors: %v", ve.Errors)
	}
	t.Logf("unregistered custom validator error: ok")
}

// --------------------------------------------------------------------------
// Nil pool -- Unique and Link rules are skipped without panic
// --------------------------------------------------------------------------

func TestValidator_NilPool_SkipsUniqueAndLink(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "NilPoolTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "code",     "field_type": "Data",  "label": "Code",     "unique": true},
			{"name": "customer", "field_type": "Link",  "label": "Customer", "options": "Customer"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("code", "ACME-001"))
	assertNoError(t, doc.Set("customer", "CUST-001"))

	v := document.NewValidator()
	// pool is nil -- unique and link checks must be silently skipped, no panic.
	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Errorf("expected no error when pool is nil (unique/link skipped); got: %v", err)
	}
	t.Logf("nil pool: unique and link checks skipped without error")
}

// --------------------------------------------------------------------------
// Regression: layout fields are skipped
// --------------------------------------------------------------------------

func TestValidator_SkipsLayoutFields(t *testing.T) {
	mt := mustCompile(t, `{
		"name": "LayoutTest", "module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [
			{"name": "section1", "field_type": "SectionBreak", "label": "Section"},
			{"name": "title",    "field_type": "Data",         "label": "Title"}
		]
	}`)
	doc := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc.Set("title", "Hello"))
	v := document.NewValidator()

	err := validateDoc(t, v, newValidatorCtx(), doc)
	if err != nil {
		t.Errorf("expected no error; layout fields must be skipped; got: %v", err)
	}
	t.Logf("layout fields skipped: ok")
}
