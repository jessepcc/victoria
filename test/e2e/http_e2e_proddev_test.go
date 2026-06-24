//go:build !dev

package e2e

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jessepcc/victoria/internal/app"
	"github.com/jessepcc/victoria/internal/httpapi"
	"github.com/jessepcc/victoria/internal/store/memory"
)

// TestHTTPDevEndpointsAbsentWithoutDevTag verifies the build-tag refactor: a
// binary built without `-tags dev` must NOT mount /admin/dev/* routes. This
// protects against the previous env-var-only gate where a misconfigured prod
// (VICTORIA_DEV_ENDPOINTS=1 leaked) could activate operator-impersonation
// endpoints. With build-tag gating the routes literally don't exist in the
// compiled binary.
func TestHTTPDevEndpointsAbsentWithoutDevTag(t *testing.T) {
	application := app.New(memory.New())
	server := httptest.NewServer(httpapi.New(application, nil).Handler())
	t.Cleanup(server.Close)
	for _, path := range []string{"/admin/dev/whatsapp/inbound", "/admin/dev/whatsapp/session-status"} {
		req, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("dev route %s returned status %d in non-dev build (want 404/405 — must not be mounted)", path, res.StatusCode)
		}
	}
}
