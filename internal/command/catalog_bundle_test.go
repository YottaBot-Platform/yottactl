package command

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCatalogBundleStatus(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v1/automation-platform/catalog/status" || r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("wrong request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"loader_state":"pending_unobserved"}`))
	}))
	defer server.Close()
	err, out := runCLI(t, server, "catalog", "bundle", "status")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(out, "pending_unobserved") {
		t.Fatalf("calls=%d output=%s", calls, out)
	}
	root := NewRoot()
	catalog := findSubcommand(root, "catalog")
	for _, name := range []string{"models", "orchestrators", "tools", "bundle"} {
		if findSubcommand(catalog, name) == nil {
			t.Fatalf("existing Catalog command %s lost", name)
		}
	}
	if findSubcommand(root, "tools-catalog") == nil {
		t.Fatal("tools-catalog lost")
	}
}
