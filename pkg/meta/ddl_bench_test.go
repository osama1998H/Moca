package meta

import (
	"fmt"
	"testing"
)

var ddlBenchmarkSink []DDLStatement

func BenchmarkGenerateTableDDL(b *testing.B) {
	for _, fieldCount := range []int{10, 50, 100} {
		b.Run(fmt.Sprintf("Fields_%d", fieldCount), func(b *testing.B) {
			mt := benchmarkDDLMetaType(fieldCount)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				stmts := GenerateTableDDL(mt)
				if len(stmts) == 0 {
					b.Fatal("GenerateTableDDL returned no statements")
				}
				ddlBenchmarkSink = stmts
			}
		})
	}
}

func benchmarkDDLMetaType(fieldCount int) *MetaType {
	fields := make([]FieldDef, 0, fieldCount)
	for i := 0; i < fieldCount; i++ {
		name := fmt.Sprintf("field_%03d", i)
		switch i % 6 {
		case 0:
			fields = append(fields, FieldDef{Name: name, Label: "Data", FieldType: FieldTypeData})
		case 1:
			fields = append(fields, FieldDef{Name: name, Label: "Int", FieldType: FieldTypeInt, DBIndex: true})
		case 2:
			fields = append(fields, FieldDef{Name: name, Label: "Currency", FieldType: FieldTypeCurrency})
		case 3:
			fields = append(fields, FieldDef{Name: name, Label: "Date", FieldType: FieldTypeDate})
		case 4:
			fields = append(fields, FieldDef{Name: name, Label: "Select", FieldType: FieldTypeSelect, Options: "A\nB\nC"})
		default:
			fields = append(fields, FieldDef{Name: name, Label: "JSON", FieldType: FieldTypeJSON})
		}
	}

	return &MetaType{
		Name:   fmt.Sprintf("BenchDDL_%d", fieldCount),
		Module: "bench",
		NamingRule: NamingStrategy{
			Rule: NamingAutoIncrement,
		},
		Fields: fields,
	}
}
