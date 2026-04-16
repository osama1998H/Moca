package meta_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

var compilerBenchmarkSink *meta.MetaType

func BenchmarkCompile(b *testing.B) {
	for _, tc := range []struct {
		name       string
		fieldCount int
	}{
		{"Simple", 5},
		{"Complex", 50},
		{"Large", 100},
	} {
		b.Run(tc.name, func(b *testing.B) {
			jsonBytes := benchmarkCompilerJSON(tc.name, tc.fieldCount)
			b.SetBytes(int64(len(jsonBytes)))

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				mt, err := meta.Compile(jsonBytes)
				if err != nil {
					b.Fatalf("Compile(%s): %v", tc.name, err)
				}
				compilerBenchmarkSink = mt
			}
		})
	}
}

func benchmarkCompilerJSON(scenario string, fieldCount int) []byte {
	fields := make([]map[string]any, 0, fieldCount)
	for i := 0; i < fieldCount; i++ {
		f := map[string]any{
			"name":       fmt.Sprintf("field_%03d", i),
			"label":      fmt.Sprintf("Field %d", i),
			"field_type": "Data",
		}
		switch i % 8 {
		case 0:
			f["field_type"] = "Data"
			f["required"] = true
			f["max_length"] = 140
		case 1:
			f["field_type"] = "Int"
			f["required"] = true
		case 2:
			f["field_type"] = "Currency"
		case 3:
			f["field_type"] = "Date"
			f["required"] = true
		case 4:
			f["field_type"] = "Select"
			f["options"] = "Draft\nSubmitted\nCancelled"
		case 5:
			f["field_type"] = "Link"
			f["options"] = "Customer"
			f["db_index"] = true
		case 6:
			f["field_type"] = "LongText"
		default:
			f["field_type"] = "JSON"
		}
		fields = append(fields, f)
	}

	doc := map[string]any{
		"name":        fmt.Sprintf("BenchCompile_%s", scenario),
		"module":      "bench",
		"label":       fmt.Sprintf("Bench Compile %s", scenario),
		"naming_rule": map[string]any{"rule": "uuid"},
		"title_field": "field_000",
		"sort_field":  "creation",
		"sort_order":  "desc",
		"fields":      fields,
	}

	data, err := json.Marshal(doc)
	if err != nil {
		panic(fmt.Sprintf("benchmarkCompilerJSON: %v", err))
	}
	return data
}
