package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/moca-framework/moca/pkg/document"
	"github.com/moca-framework/moca/pkg/meta"
)

// successEnvelope wraps a single value in the standard {"data": ...} envelope.
type successEnvelope struct {
	Data any `json:"data"`
}

// listEnvelope wraps a list result with pagination metadata.
type listEnvelope struct {
	Data any          `json:"data"`
	Meta listPaginate `json:"meta"`
}

// listPaginate holds pagination info returned with list responses.
type listPaginate struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// errorEnvelope wraps an error in the standard {"error": ...} envelope.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// errorBody is the inner error object.
type errorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details []fieldErrorDetail `json:"details,omitempty"`
}

// fieldErrorDetail is a structured field-level validation failure.
type fieldErrorDetail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Rule    string `json:"rule"`
}

// writeSuccess writes a single-document success response.
// Status is typically 200 (OK) or 201 (Created).
func writeSuccess(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(successEnvelope{Data: data}) //nolint:errcheck
}

// writeListSuccess writes a list response with pagination metadata.
func writeListSuccess(w http.ResponseWriter, data any, total, limit, offset int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(listEnvelope{ //nolint:errcheck
		Data: data,
		Meta: listPaginate{Total: total, Limit: limit, Offset: offset},
	})
}

// writeError writes a JSON error response matching the envelope used by middleware.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorEnvelope{ //nolint:errcheck
		Error: errorBody{Code: code, Message: message},
	})
}

// writeValidationError writes a 422 response with structured field-level details.
func writeValidationError(w http.ResponseWriter, ve *document.ValidationError) {
	details := make([]fieldErrorDetail, len(ve.Errors))
	for i, fe := range ve.Errors {
		details[i] = fieldErrorDetail{
			Field:   fe.Field,
			Message: fe.Message,
			Rule:    fe.Rule,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	json.NewEncoder(w).Encode(errorEnvelope{ //nolint:errcheck
		Error: errorBody{
			Code:    "VALIDATION_ERROR",
			Message: ve.Error(),
			Details: details,
		},
	})
}

// mapErrorResponse inspects err and writes the appropriate HTTP error response.
// It returns true if the error was recognised and handled, false otherwise.
// When false is returned, nothing has been written to w.
func mapErrorResponse(w http.ResponseWriter, err error) bool {
	var docNotFound *document.DocNotFoundError
	if errors.As(err, &docNotFound) {
		writeError(w, http.StatusNotFound, "DOC_NOT_FOUND", docNotFound.Error())
		return true
	}

	var validationErr *document.ValidationError
	if errors.As(err, &validationErr) {
		writeValidationError(w, validationErr)
		return true
	}

	var permDenied *PermissionDeniedError
	if errors.As(err, &permDenied) {
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED", permDenied.Error())
		return true
	}

	if errors.Is(err, meta.ErrMetaTypeNotFound) {
		writeError(w, http.StatusNotFound, "DOCTYPE_NOT_FOUND", "doctype not found")
		return true
	}

	return false
}
