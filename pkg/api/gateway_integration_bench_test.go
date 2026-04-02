//go:build integration

package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/osama1998H/moca/internal/testutil/bench"
	"github.com/osama1998H/moca/pkg/meta"
)

var gatewayBenchmarkStatusSink int

func BenchmarkGatewayHandler_FullChain(b *testing.B) {
	env := bench.NewIntegrationEnv(b, "api_gateway")
	env.RequireRedis(b)

	mt := env.RegisterMetaType(b, bench.SimpleDocType("BenchAPIOrder"))
	seeded, err := env.DocManager().Insert(env.DocContext(), mt.Name, bench.SimpleDocValues(1))
	if err != nil {
		b.Fatalf("seed API benchmark document: %v", err)
	}

	gateway := env.NewGatewayBundle(b, &meta.RateLimitConfig{
		MaxRequests: 1,
		Window:      time.Nanosecond,
	})
	path := fmt.Sprintf("/api/v1/resource/%s/%s", mt.Name, seeded.Name())

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Moca-Site", env.SiteName)
		rec := httptest.NewRecorder()
		b.StartTimer()

		gateway.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("Gateway.Handler status = %d, body = %s", rec.Code, rec.Body.String())
		}
		gatewayBenchmarkStatusSink = rec.Code
	}
}
