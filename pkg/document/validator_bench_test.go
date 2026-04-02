package document_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
)

var validatorBenchmarkDocSink *document.DynamicDoc

func BenchmarkValidateDoc(b *testing.B) {
	for _, fieldCount := range []int{10, 50, 100} {
		b.Run(fmt.Sprintf("Fields_%d", fieldCount), func(b *testing.B) {
			mt := benchmarkValidatorMetaType(fieldCount)
			ctx := document.NewDocContext(context.Background(), nil, nil)
			validator := document.NewValidator()
			docs := make([]*document.DynamicDoc, b.N)
			for i := 0; i < b.N; i++ {
				docs[i] = benchmarkValidatorDoc(b, mt, i)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				doc := docs[i]
				if err := validator.ValidateDoc(ctx, doc, nil); err != nil {
					b.Fatalf("ValidateDoc(%d): %v", i, err)
				}
				validatorBenchmarkDocSink = doc
			}
		})
	}
}

func benchmarkValidatorMetaType(fieldCount int) *meta.MetaType {
	fields := make([]meta.FieldDef, 0, fieldCount)
	for i := 0; i < fieldCount; i++ {
		name := fmt.Sprintf("field_%03d", i)
		switch i % 9 {
		case 0:
			fields = append(fields, meta.FieldDef{
				Name:      name,
				Label:     "Data",
				FieldType: meta.FieldTypeData,
				Required:  true,
				MaxLength: 16,
			})
		case 1:
			fields = append(fields, meta.FieldDef{
				Name:      name,
				Label:     "Int",
				FieldType: meta.FieldTypeInt,
				Required:  true,
				MinValue:  float64Ptr(1),
				MaxValue:  float64Ptr(1_000_000_000),
			})
		case 2:
			fields = append(fields, meta.FieldDef{
				Name:      name,
				Label:     "Check",
				FieldType: meta.FieldTypeCheck,
			})
		case 3:
			fields = append(fields, meta.FieldDef{
				Name:      name,
				Label:     "Currency",
				FieldType: meta.FieldTypeCurrency,
				MinValue:  float64Ptr(0),
				MaxValue:  float64Ptr(1_000_000_000),
			})
		case 4:
			fields = append(fields, meta.FieldDef{
				Name:      name,
				Label:     "Date",
				FieldType: meta.FieldTypeDate,
				Required:  true,
			})
		case 5:
			fields = append(fields, meta.FieldDef{
				Name:      name,
				Label:     "Datetime",
				FieldType: meta.FieldTypeDatetime,
			})
		case 6:
			fields = append(fields, meta.FieldDef{
				Name:      name,
				Label:     "Time",
				FieldType: meta.FieldTypeTime,
			})
		case 7:
			fields = append(fields, meta.FieldDef{
				Name:      name,
				Label:     "JSON",
				FieldType: meta.FieldTypeJSON,
			})
		default:
			fields = append(fields, meta.FieldDef{
				Name:      name,
				Label:     "Status",
				FieldType: meta.FieldTypeSelect,
				Options:   "Draft\nSubmitted\nCancelled",
			})
		}
	}

	return &meta.MetaType{
		Name:   fmt.Sprintf("BenchValidate_%d", fieldCount),
		Module: "bench",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: fields,
	}
}

func benchmarkValidatorDoc(b *testing.B, mt *meta.MetaType, seq int) *document.DynamicDoc {
	b.Helper()

	doc := document.NewDynamicDoc(mt, nil, true)
	for i := range mt.Fields {
		fd := mt.Fields[i]
		if err := doc.Set(fd.Name, benchmarkValidatorValue(i, seq)); err != nil {
			b.Fatalf("Set(%q): %v", fd.Name, err)
		}
	}
	return doc
}

func benchmarkValidatorValue(index, seq int) any {
	switch index % 9 {
	case 0:
		return seq*1000 + index
	case 1:
		return fmt.Sprintf("%d", 100+seq+index)
	case 2:
		if (seq+index)%2 == 0 {
			return "true"
		}
		return "false"
	case 3:
		return fmt.Sprintf("%d.%02d", 100+index, (seq+index)%100)
	case 4:
		return "2026-04-03"
	case 5:
		return "2026-04-03T15:04:05Z"
	case 6:
		return "15:04:05"
	case 7:
		return fmt.Sprintf(`{"seq":%d,"index":%d}`, seq, index)
	default:
		return "Draft"
	}
}

func float64Ptr(v float64) *float64 {
	return &v
}
