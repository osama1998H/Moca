package meta

import (
	"encoding/json"
	"fmt"
	"os"
)

// MigrateFileToTree reads a DocType JSON file at path, checks if it is already
// in tree-native format, and if not, converts it from flat format to tree-native
// format by calling FlatToTree. The result is written back to the same file.
//
// Return values:
//   - (false, nil)  — file was already tree-native (layout key present); no change
//   - (true, nil)   — file was flat; successfully migrated and overwritten
//   - (false, err)  — an error occurred (read, parse, or write failure)
//
// The function is idempotent: calling it twice on the same file is safe.
func MigrateFileToTree(path string) (bool, error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return false, fmt.Errorf("migrate_layout: read %q: %w", path, readErr)
	}

	// Parse into a raw map so we can probe the layout key and later preserve
	// all top-level properties (name, module, permissions, api_config, etc.)
	// without enumerating them explicitly.
	var raw map[string]json.RawMessage
	if parseErr := json.Unmarshal(data, &raw); parseErr != nil {
		return false, fmt.Errorf("migrate_layout: parse JSON %q: %w", path, parseErr)
	}

	// Idempotency check: if "layout" key exists and its value starts with '{'
	// the file is already in tree-native format — nothing to do.
	if layoutRaw, ok := raw["layout"]; ok && len(layoutRaw) > 0 && layoutRaw[0] == '{' {
		return false, nil
	}

	// Parse the flat fields array.
	var fields []FieldDef
	if fieldsRaw, ok := raw["fields"]; ok {
		if fieldsErr := json.Unmarshal(fieldsRaw, &fields); fieldsErr != nil {
			return false, fmt.Errorf("migrate_layout: parse fields in %q: %w", path, fieldsErr)
		}
	}

	// Convert flat field list to tree layout + fields map.
	layoutTree, fieldsMap := FlatToTree(fields)

	// Build the tree-native "fields" map as JSON.
	// For each entry, remove the "name" key — in tree-native format the map key
	// is the field name; embedding it inside the value is redundant.
	treeFields := make(map[string]json.RawMessage, len(fieldsMap))
	for fieldName, fd := range fieldsMap {
		fdJSON, marshalErr := json.Marshal(fd)
		if marshalErr != nil {
			return false, fmt.Errorf("migrate_layout: marshal field %q: %w", fieldName, marshalErr)
		}

		// Strip the "name" key from the field object.
		var fdMap map[string]json.RawMessage
		if reParseErr := json.Unmarshal(fdJSON, &fdMap); reParseErr != nil {
			return false, fmt.Errorf("migrate_layout: re-parse field %q: %w", fieldName, reParseErr)
		}
		delete(fdMap, "name")

		cleanJSON, reMarshalErr := json.Marshal(fdMap)
		if reMarshalErr != nil {
			return false, fmt.Errorf("migrate_layout: re-marshal field %q: %w", fieldName, reMarshalErr)
		}
		treeFields[fieldName] = cleanJSON
	}

	// Marshal layout tree.
	layoutJSON, layoutMarshalErr := json.Marshal(layoutTree)
	if layoutMarshalErr != nil {
		return false, fmt.Errorf("migrate_layout: marshal layout: %w", layoutMarshalErr)
	}

	// Marshal the tree-native fields map.
	fieldsJSON, fieldsMarshalErr := json.Marshal(treeFields)
	if fieldsMarshalErr != nil {
		return false, fmt.Errorf("migrate_layout: marshal fields map: %w", fieldsMarshalErr)
	}

	// Replace "layout" and "fields" in the raw map.
	// Any other keys (name, module, label, description, naming_rule, permissions,
	// api_config, is_submittable, track_changes, etc.) are preserved as-is.
	raw["layout"] = layoutJSON
	raw["fields"] = fieldsJSON

	// Encode to indented JSON and write back.
	out, outMarshalErr := json.MarshalIndent(raw, "", "  ")
	if outMarshalErr != nil {
		return false, fmt.Errorf("migrate_layout: marshal output: %w", outMarshalErr)
	}

	// Append a trailing newline (conventional for text files).
	out = append(out, '\n')

	if writeErr := os.WriteFile(path, out, 0o644); writeErr != nil {
		return false, fmt.Errorf("migrate_layout: write %q: %w", path, writeErr)
	}

	return true, nil
}
