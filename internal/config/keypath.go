package config

import "strings"

// GetByPath traverses nested maps by dot-separated key path.
// Returns (value, true) if found, (nil, false) if any segment is missing
// or an intermediate value is not a map.
func GetByPath(data map[string]any, key string) (any, bool) {
	if key == "" || data == nil {
		return nil, false
	}

	parts := strings.Split(key, ".")
	current := any(data)

	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[part]
		if !ok {
			return nil, false
		}
	}

	return current, true
}

// SetByPath sets a value at the dot-separated key path, creating intermediate
// map[string]any as needed. Overwrites existing non-map values at intermediate
// segments.
func SetByPath(data map[string]any, key string, value any) {
	if key == "" || data == nil {
		return
	}

	parts := strings.Split(key, ".")
	current := data

	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part]
		if !ok {
			child := make(map[string]any)
			current[part] = child
			current = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			child = make(map[string]any)
			current[part] = child
		}
		current = child
	}

	current[parts[len(parts)-1]] = value
}

// RemoveByPath removes the key at the dot-separated path.
// Returns true if the key existed and was removed.
func RemoveByPath(data map[string]any, key string) bool {
	if key == "" || data == nil {
		return false
	}

	parts := strings.Split(key, ".")
	current := data

	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part]
		if !ok {
			return false
		}
		child, ok := next.(map[string]any)
		if !ok {
			return false
		}
		current = child
	}

	last := parts[len(parts)-1]
	if _, ok := current[last]; !ok {
		return false
	}
	delete(current, last)
	return true
}

// FlattenMap flattens a nested map to dot-notation keys.
// For example, {"a": {"b": 1}} becomes {"a.b": 1}.
// Non-map values (including slices) are treated as leaf values.
// Pass "" as prefix for the root call.
func FlattenMap(data map[string]any, prefix string) map[string]any {
	result := make(map[string]any)
	flattenInto(result, data, prefix)
	return result
}

func flattenInto(result, data map[string]any, prefix string) {
	for k, v := range data {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}

		if child, ok := v.(map[string]any); ok {
			flattenInto(result, child, fullKey)
		} else {
			result[fullKey] = v
		}
	}
}

// MergeMaps deep-merges overlay into base, returning a new map.
// For nested maps, merge recursively. For other types, overlay wins.
func MergeMaps(base, overlay map[string]any) map[string]any {
	result := make(map[string]any, len(base))

	for k, v := range base {
		result[k] = v
	}

	for k, ov := range overlay {
		bv, exists := result[k]
		if !exists {
			result[k] = ov
			continue
		}

		bMap, bIsMap := bv.(map[string]any)
		oMap, oIsMap := ov.(map[string]any)
		if bIsMap && oIsMap {
			result[k] = MergeMaps(bMap, oMap)
		} else {
			result[k] = ov
		}
	}

	return result
}
