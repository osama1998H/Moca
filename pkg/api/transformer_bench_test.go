package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

var transformerBenchmarkBodySink map[string]any

func BenchmarkTransformerChain_Request(b *testing.B) {
	mt, version := benchmarkTransformerMeta()
	chain := NewTransformerChain(mt, version)
	payloads := make([]map[string]any, b.N)
	for i := 0; i < b.N; i++ {
		payloads[i] = map[string]any{
			"client":        fmt.Sprintf("Customer-%06d", i),
			"total_amount":  float64(100 + i),
			"status":        "Draft",
			"created_by":    "bench-admin",
			"auto_stamp":    fmt.Sprintf("STAMP-%06d", i),
			"internal_note": fmt.Sprintf("note-%06d", i),
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		body, err := chain.TransformRequest(context.Background(), mt, payloads[i])
		if err != nil {
			b.Fatalf("TransformRequest(%d): %v", i, err)
		}
		transformerBenchmarkBodySink = body
	}
}

func BenchmarkTransformerChain_Response(b *testing.B) {
	mt, version := benchmarkTransformerMeta()
	chain := NewTransformerChain(mt, version)
	ctx := WithOperationType(context.Background(), OpList)
	payloads := make([]map[string]any, b.N)
	for i := 0; i < b.N; i++ {
		payloads[i] = map[string]any{
			"name":          fmt.Sprintf("DOC-%06d", i),
			"customer_name": fmt.Sprintf("Customer-%06d", i),
			"grand_total":   float64(100 + i),
			"status":        "Draft",
			"created_by":    "bench-admin",
			"modified_by":   "bench-admin",
			"internal_note": fmt.Sprintf("secret-%06d", i),
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		body, err := chain.TransformResponse(ctx, mt, payloads[i])
		if err != nil {
			b.Fatalf("TransformResponse(%d): %v", i, err)
		}
		transformerBenchmarkBodySink = body
	}
}

func benchmarkTransformerMeta() (*meta.MetaType, *meta.APIVersion) {
	mt := &meta.MetaType{
		Name:   "BenchTransformDoc",
		Module: "bench",
		Fields: []meta.FieldDef{
			{Name: "customer_name", FieldType: meta.FieldTypeData, InAPI: true, APIAlias: "customer"},
			{Name: "grand_total", FieldType: meta.FieldTypeCurrency, InAPI: true, APIAlias: "total"},
			{Name: "status", FieldType: meta.FieldTypeSelect, InAPI: true, Options: "Draft\nSubmitted\nCancelled"},
			{Name: "created_by", FieldType: meta.FieldTypeData, InAPI: true, ReadOnly: true},
			{Name: "auto_stamp", FieldType: meta.FieldTypeData, InAPI: true, APIReadOnly: true},
			{Name: "internal_note", FieldType: meta.FieldTypeLongText, InAPI: false},
			{Name: "modified_by", FieldType: meta.FieldTypeData, InAPI: true},
		},
		APIConfig: &meta.APIConfig{
			Enabled:       true,
			DefaultFields: []string{"name", "customer_name", "grand_total", "status"},
			AlwaysInclude: []string{"name"},
			Versions: []meta.APIVersion{
				{
					Version:      "v2",
					Status:       "active",
					FieldMapping: map[string]string{"client": "customer_name", "total_amount": "grand_total"},
					ExcludeFields: []string{
						"status",
						"modified_by",
					},
				},
			},
		},
	}

	return mt, &mt.APIConfig.Versions[0]
}
