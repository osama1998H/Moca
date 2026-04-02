package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
)

func TestWriteSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	writeSuccess(w, http.StatusOK, map[string]any{"name": "DOC-001"})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var env successEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data is %T, want map", env.Data)
	}
	if data["name"] != "DOC-001" {
		t.Errorf("data.name = %v, want DOC-001", data["name"])
	}
}

func TestWriteSuccess_Created(t *testing.T) {
	w := httptest.NewRecorder()
	writeSuccess(w, http.StatusCreated, map[string]any{"name": "NEW-001"})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestWriteListSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	items := []map[string]any{{"name": "A"}, {"name": "B"}}
	writeListSuccess(w, items, 42, 10, 20)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var env listEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Meta.Total != 42 {
		t.Errorf("meta.total = %d, want 42", env.Meta.Total)
	}
	if env.Meta.Limit != 10 {
		t.Errorf("meta.limit = %d, want 10", env.Meta.Limit)
	}
	if env.Meta.Offset != 20 {
		t.Errorf("meta.offset = %d, want 20", env.Meta.Offset)
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "BAD_REQUEST", "something broke")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var env errorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Error.Code != "BAD_REQUEST" {
		t.Errorf("error.code = %q, want BAD_REQUEST", env.Error.Code)
	}
	if env.Error.Message != "something broke" {
		t.Errorf("error.message = %q, want 'something broke'", env.Error.Message)
	}
}

func TestWriteValidationError(t *testing.T) {
	w := httptest.NewRecorder()
	ve := &document.ValidationError{
		Errors: []document.FieldError{
			{Field: "email", Message: "is required", Rule: "required"},
			{Field: "age", Message: "must be positive", Rule: "min_value"},
		},
	}
	writeValidationError(w, ve)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var env errorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("error.code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
	if len(env.Error.Details) != 2 {
		t.Fatalf("details length = %d, want 2", len(env.Error.Details))
	}
	if env.Error.Details[0].Field != "email" {
		t.Errorf("details[0].field = %q, want email", env.Error.Details[0].Field)
	}
}

func TestMapErrorResponse_DocNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	err := &document.DocNotFoundError{Doctype: "Item", Name: "ITEM-001"}
	if !mapErrorResponse(w, err) {
		t.Fatal("expected true")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestMapErrorResponse_ValidationError(t *testing.T) {
	w := httptest.NewRecorder()
	err := &document.ValidationError{
		Errors: []document.FieldError{{Field: "x", Message: "bad", Rule: "custom"}},
	}
	if !mapErrorResponse(w, err) {
		t.Fatal("expected true")
	}
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestMapErrorResponse_PermissionDenied(t *testing.T) {
	w := httptest.NewRecorder()
	err := &PermissionDeniedError{User: "bob", Doctype: "Item", Perm: "write"}
	if !mapErrorResponse(w, err) {
		t.Fatal("expected true")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestMapErrorResponse_MetaTypeNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	if !mapErrorResponse(w, meta.ErrMetaTypeNotFound) {
		t.Fatal("expected true")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestMapErrorResponse_WrappedMetaTypeNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	wrapped := errors.New("outer: " + meta.ErrMetaTypeNotFound.Error())
	// This is a non-wrapped error — should not match.
	if mapErrorResponse(w, wrapped) {
		t.Fatal("expected false for non-wrapped error")
	}
}

func TestMapErrorResponse_UnknownError(t *testing.T) {
	w := httptest.NewRecorder()
	if mapErrorResponse(w, errors.New("mystery")) {
		t.Fatal("expected false for unknown error")
	}
}
