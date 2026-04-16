package api

import (
	"errors"
	"fmt"
	"regexp"
	"unicode"

	"github.com/osama1998H/moca/pkg/meta"
)

// reFieldName matches valid snake_case field names: starts with a lowercase letter,
// followed by lowercase letters, digits, or underscores.
var reFieldName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// reAppModuleName matches valid app and module names: lowercase letter start,
// followed by lowercase letters, digits, underscores, or hyphens.
var reAppModuleName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// reservedFieldNames contains field names that are managed internally by the
// framework and must not be used as user-defined field names.
var reservedFieldNames = map[string]bool{
	"name":           true,
	"created_at":     true,
	"modified_at":    true,
	"owner":          true,
	"_extra":         true,
	"modified_by":    true,
	"creation":       true,
	"modified":       true,
	"docstatus":      true,
	"idx":            true,
	"workflow_state": true,
	"parent":         true,
	"parenttype":     true,
	"parentfield":    true,
}

// ValidateDocTypeName checks that name is a valid DocType name.
//
// Rules:
//   - Must not be empty.
//   - Must start with an uppercase letter.
//   - Must contain only ASCII letters and digits (no underscores, hyphens, or spaces).
func ValidateDocTypeName(name string) error {
	if name == "" {
		return errors.New("doctype name must not be empty")
	}

	runes := []rune(name)

	if !unicode.IsUpper(runes[0]) || !unicode.IsLetter(runes[0]) {
		return errors.New("doctype name must start with an uppercase letter")
	}

	for _, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return errors.New("doctype name must contain only letters and digits")
		}
	}

	return nil
}

// ValidateFieldName checks that name is a valid field name.
//
// Rules:
//   - Must not be empty.
//   - Must match ^[a-z][a-z0-9_]*$ (snake_case starting with a lowercase letter).
//   - Must not be a reserved framework field name.
func ValidateFieldName(name string) error {
	if name == "" {
		return errors.New("field name must not be empty")
	}

	if !reFieldName.MatchString(name) {
		return errors.New("field name must match ^[a-z][a-z0-9_]*$ (snake_case starting with a lowercase letter)")
	}

	if reservedFieldNames[name] {
		return errors.New("field name " + name + " is reserved by the framework")
	}

	return nil
}

// ValidateAppName checks that name is a valid app directory name.
func ValidateAppName(name string) error {
	if !reAppModuleName.MatchString(name) {
		return errors.New("app name must match ^[a-z][a-z0-9_-]*$ (lowercase, digits, hyphens, underscores)")
	}
	return nil
}

// ValidateModuleName checks that name is a valid module directory name.
func ValidateModuleName(name string) error {
	if !reAppModuleName.MatchString(name) {
		return errors.New("module name must match ^[a-z][a-z0-9_-]*$ (lowercase, digits, hyphens, underscores)")
	}
	return nil
}

// ValidateFieldDefs checks that every field has a non-empty, recognized field_type.
func ValidateFieldDefs(fields map[string]meta.FieldDef) error {
	for name, fd := range fields {
		if fd.FieldType == "" {
			return errors.New("field '" + name + "' has no field_type")
		}
		if !meta.FieldType(fd.FieldType).IsValid() {
			return fmt.Errorf("field '%s' has unrecognized field_type %q", name, fd.FieldType)
		}
	}
	return nil
}
