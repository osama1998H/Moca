package orm_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/osama1998H/moca/pkg/orm"
)

type benchmarkMetaProvider struct {
	metas map[string]*orm.QueryMeta
}

func (p benchmarkMetaProvider) QueryMeta(_ context.Context, _ string, doctype string) (*orm.QueryMeta, error) {
	qm, ok := p.metas[doctype]
	if !ok {
		return nil, fmt.Errorf("unknown doctype %q", doctype)
	}
	return qm, nil
}

var (
	queryBenchmarkSQLSink  string
	queryBenchmarkArgsSink []any
)

func BenchmarkQueryBuilderBuild_SimpleFilter(b *testing.B) {
	ctx := context.Background()
	provider := benchmarkMetaProvider{
		metas: map[string]*orm.QueryMeta{
			"SalesOrder": {
				Name:      "SalesOrder",
				TableName: "tab_sales_order",
				ValidColumns: map[string]struct{}{
					"name":        {},
					"customer":    {},
					"status":      {},
					"grand_total": {},
					"creation":    {},
				},
				FieldTypes: map[string]string{
					"name":        "TEXT",
					"customer":    "TEXT",
					"status":      "TEXT",
					"grand_total": "NUMERIC(18,6)",
					"creation":    "TIMESTAMPTZ",
				},
			},
		},
	}
	builder := orm.NewQueryBuilder(provider, "bench").
		For("SalesOrder").
		Fields("name", "customer", "grand_total").
		Where(orm.Filter{Field: "status", Operator: orm.OpEqual, Value: "Draft"}).
		OrderBy("creation", "DESC").
		Limit(20)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sql, args, err := builder.Build(ctx)
		if err != nil {
			b.Fatalf("QueryBuilder.Build simple: %v", err)
		}
		queryBenchmarkSQLSink = sql
		queryBenchmarkArgsSink = args
	}
}

func BenchmarkQueryBuilderBuild_ComplexJoins(b *testing.B) {
	ctx := context.Background()
	provider := benchmarkMetaProvider{
		metas: map[string]*orm.QueryMeta{
			"SalesOrder": {
				Name:      "SalesOrder",
				TableName: "tab_sales_order",
				ValidColumns: map[string]struct{}{
					"name":        {},
					"status":      {},
					"customer":    {},
					"grand_total": {},
					"creation":    {},
				},
				FieldTypes: map[string]string{
					"name":        "TEXT",
					"status":      "TEXT",
					"customer":    "TEXT",
					"grand_total": "NUMERIC(18,6)",
					"creation":    "TIMESTAMPTZ",
				},
				LinkFields: map[string]string{
					"customer": "Customer",
				},
			},
			"Customer": {
				Name:      "Customer",
				TableName: "tab_customer",
				ValidColumns: map[string]struct{}{
					"name":          {},
					"customer_name": {},
					"territory":     {},
				},
				FieldTypes: map[string]string{
					"name":          "TEXT",
					"customer_name": "TEXT",
					"territory":     "TEXT",
				},
				LinkFields: map[string]string{
					"territory": "Territory",
				},
			},
			"Territory": {
				Name:      "Territory",
				TableName: "tab_territory",
				ValidColumns: map[string]struct{}{
					"name":           {},
					"territory_name": {},
					"region":         {},
				},
				FieldTypes: map[string]string{
					"name":           "TEXT",
					"territory_name": "TEXT",
					"region":         "TEXT",
				},
			},
		},
	}
	builder := orm.NewQueryBuilder(provider, "bench").
		For("SalesOrder").
		Fields("name", "customer.customer_name", "customer.territory.region", "grand_total").
		Where(
			orm.Filter{Field: "status", Operator: orm.OpIn, Value: []any{"Draft", "Submitted"}},
			orm.Filter{Field: "customer.territory.region", Operator: orm.OpEqual, Value: "EMEA"},
		).
		OrderBy("customer.customer_name", "ASC").
		Limit(50)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sql, args, err := builder.Build(ctx)
		if err != nil {
			b.Fatalf("QueryBuilder.Build joins: %v", err)
		}
		queryBenchmarkSQLSink = sql
		queryBenchmarkArgsSink = args
	}
}

func BenchmarkQueryBuilderBuild_JSONBFilter(b *testing.B) {
	ctx := context.Background()
	provider := benchmarkMetaProvider{
		metas: map[string]*orm.QueryMeta{
			"SalesOrder": {
				Name:      "SalesOrder",
				TableName: "tab_sales_order",
				ValidColumns: map[string]struct{}{
					"name":        {},
					"status":      {},
					"grand_total": {},
					"creation":    {},
				},
				FieldTypes: map[string]string{
					"name":        "TEXT",
					"status":      "TEXT",
					"grand_total": "NUMERIC(18,6)",
					"creation":    "TIMESTAMPTZ",
				},
			},
		},
	}
	builder := orm.NewQueryBuilder(provider, "bench").
		For("SalesOrder").
		Fields("name", "grand_total", "custom_segment", "lifetime_value").
		Where(
			orm.Filter{Field: "custom_segment", Operator: orm.OpEqual, Value: "enterprise"},
			orm.Filter{Field: "lifetime_value", Operator: orm.OpGreaterOrEq, Value: 1000.0},
		).
		OrderBy("creation", "DESC").
		Limit(25)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sql, args, err := builder.Build(ctx)
		if err != nil {
			b.Fatalf("QueryBuilder.Build JSONB: %v", err)
		}
		queryBenchmarkSQLSink = sql
		queryBenchmarkArgsSink = args
	}
}
