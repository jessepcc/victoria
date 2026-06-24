//go:build dev

package e2e

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jessepcc/victoria/internal/app"
	"github.com/jessepcc/victoria/internal/domain"
	"github.com/jessepcc/victoria/internal/httpapi"
	"github.com/jessepcc/victoria/internal/store/memory"
)

// TestHTTPDevEndpointsMountedWithDevTag is the complement of the prod-build test
// (http_e2e_proddev_test.go): with `-tags dev` the /admin/dev/* simulation
// routes MUST be mounted and functional. Together the two tests pin both sides
// of the build-tag boundary. Run with: go test -tags dev ./test/e2e
func TestHTTPDevEndpointsMountedWithDevTag(t *testing.T) {
	application := app.New(memory.New())
	apiServer := httpapi.New(application, nil)
	apiServer.SetAdminToken(testAdminToken)
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	var provision struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	adminPost(t, server.URL+"/admin/tenants", map[string]any{
		"name":            "Dev Roofing",
		"vertical":        "roofing",
		"provider_number": "+61400000900",
		"operator_id":     "op_telegram:dev",
	}, &provision, http.StatusCreated)

	// Record A0 (read-only) consent so a WhatsApp binding exists for the
	// inbound code path the dev shim drives.
	post(t, server.URL+"/channel-bindings/whatsapp/consent", provision.Tenant.ID, map[string]any{
		"inbound_mode":       "read_only",
		"draft_delivery_jid": "61400000900@s.whatsapp.net",
	}, nil, http.StatusOK)

	noAuth := map[string]string{"Content-Type": "application/json"}

	// Dev-only routes carry no auth and must be mounted (404 in prod builds).
	postWithHeaders(t, server.URL+"/admin/dev/whatsapp/session-status", noAuth, map[string]any{
		"tenant_id": provision.Tenant.ID,
		"status":    "active",
	}, nil, http.StatusAccepted)

	// Operator self-message ("add customer ...") through the real
	// HandleWhatsAppInbound path, with the impersonation flag the dev shim
	// exists to exercise. A 202 confirms the route is mounted and the command
	// was processed without error.
	postWithHeaders(t, server.URL+"/admin/dev/whatsapp/inbound", noAuth, map[string]any{
		"tenant_id":           provision.Tenant.ID,
		"sender_jid":          "61400000900@s.whatsapp.net",
		"is_from_me":          true,
		"provider_message_id": "dev-msg-1",
		"free_text":           "add customer +85299999999",
	}, nil, http.StatusAccepted)
}
