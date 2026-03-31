package output

import (
	"encoding/json"
	"io"
)

// WriteJSON marshals v as indented JSON and writes it to w.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// WriteJSONCompact marshals v as compact JSON (no indentation) and writes it to w.
func WriteJSONCompact(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}
