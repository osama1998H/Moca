//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/osama1998H/moca/pkg/testutils"
	"github.com/osama1998H/moca/pkg/testutils/factory"
)

func TestRESTCreate(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("APIOrder")
	env.RegisterMetaType(t, mt)

	bundle := env.NewGatewayBundle(t, nil)

	body := `{"customer_name":"API Customer","status":"Open","grand_total":100.50}`
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/resource/%s/%s", env.SiteName, "APIOrder"),
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Moca-User", env.User.Email)

	w := httptest.NewRecorder()
	bundle.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("expected 200/201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	data, _ := resp["data"].(map[string]any)
	if data == nil {
		t.Fatal("response should have 'data' field")
	}
	if data["name"] == nil || data["name"] == "" {
		t.Fatal("created document should have a name")
	}
}

func TestRESTGet(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("APIGetOrder")
	env.RegisterMetaType(t, mt)

	// Create a document directly.
	doc := env.NewTestDoc(t, "APIGetOrder", factory.SimpleDocValues(1))

	bundle := env.NewGatewayBundle(t, nil)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/resource/%s/%s/%s", env.SiteName, "APIGetOrder", doc.Name()),
		nil)
	req.Header.Set("X-Moca-User", env.User.Email)

	w := httptest.NewRecorder()
	bundle.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, _ := resp["data"].(map[string]any)
	if data == nil {
		t.Fatal("response should have 'data' field")
	}
}

func TestRESTList(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("APIListOrder")
	env.RegisterMetaType(t, mt)

	// Create 3 documents.
	for i := 1; i <= 3; i++ {
		env.NewTestDoc(t, "APIListOrder", factory.SimpleDocValues(i))
	}

	bundle := env.NewGatewayBundle(t, nil)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/resource/%s/%s", env.SiteName, "APIListOrder"),
		nil)
	req.Header.Set("X-Moca-User", env.User.Email)

	w := httptest.NewRecorder()
	bundle.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRESTDelete(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("APIDelOrder")
	env.RegisterMetaType(t, mt)

	doc := env.NewTestDoc(t, "APIDelOrder", factory.SimpleDocValues(1))

	bundle := env.NewGatewayBundle(t, nil)

	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/v1/resource/%s/%s/%s", env.SiteName, "APIDelOrder", doc.Name()),
		nil)
	req.Header.Set("X-Moca-User", env.User.Email)

	w := httptest.NewRecorder()
	bundle.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("expected 200/204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRESTPagination(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("APIPageOrder")
	env.RegisterMetaType(t, mt)

	// Create 10 documents.
	for i := 1; i <= 10; i++ {
		env.NewTestDoc(t, "APIPageOrder", factory.SimpleDocValues(i))
	}

	bundle := env.NewGatewayBundle(t, nil)

	// Request page with limit 3.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/resource/%s/%s?limit=3&offset=0", env.SiteName, "APIPageOrder"),
		nil)
	req.Header.Set("X-Moca-User", env.User.Email)

	w := httptest.NewRecorder()
	bundle.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
