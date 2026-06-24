package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jessepcc/victoria/internal/app"
	"github.com/jessepcc/victoria/internal/domain"
	"github.com/jessepcc/victoria/internal/httpapi"
	"github.com/jessepcc/victoria/internal/store/memory"
)

func TestHTTPGoldenCorrectionLoop(t *testing.T) {
	application := app.New(memory.New())
	apiServer := httpapi.New(application, nil)
	apiServer.SetGatewayInboundToken("test-gw-token")
	apiServer.SetAdminToken(testAdminToken)
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	var provision struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	adminPost(t, server.URL+"/admin/tenants", map[string]any{
		"name":            "ABC Roofing",
		"vertical":        "roofing",
		"provider_number": "+61400000000",
		"operator_id":     "op_telegram:owner",
	}, &provision, http.StatusCreated)
	if provision.Tenant.ID == "" {
		t.Fatal("tenant id missing")
	}

	var firstPacket domain.ReviewPacket
	for i := 0; i < 3; i++ {
		var start struct {
			ReviewPacket domain.ReviewPacket `json:"review_packet"`
		}
		post(t, server.URL+"/cases", provision.Tenant.ID, map[string]any{
			"workflow_slug": "quote_drafting",
			"mode":          "sandbox",
			"payload": map[string]any{
				"sandbox":         true,
				"case_name":       fmt.Sprintf("case-%d", i),
				"client_type":     "new",
				"photos_complete": false,
			},
		}, &start, http.StatusCreated)
		if i == 0 {
			firstPacket = start.ReviewPacket
		}
		postWithHeaders(t, server.URL+"/gateway/inbound", map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer gw:test-gw-token",
		}, map[string]any{
			"channel":           "telegram",
			"provider_number":   "+61400000000",
			"packet_id":         start.ReviewPacket.PacketID,
			"source_message_id": fmt.Sprintf("msg-%d", i),
			"action_button":     "wrong_action",
			"free_text":         "Should have held and asked for more photos.",
			"follow_up_answer":  "always when client is new and photos are incomplete",
		}, nil, http.StatusAccepted)
	}
	if firstPacket.PacketID == "" {
		t.Fatal("first packet missing")
	}

	var listed struct {
		Candidates []domain.RuleCandidate `json:"candidates"`
	}
	get(t, server.URL+"/candidates", provision.Tenant.ID, &listed, http.StatusOK)
	if len(listed.Candidates) != 1 || listed.Candidates[0].Status != "under_review" {
		t.Fatalf("candidates = %+v, want one under_review", listed.Candidates)
	}

	var promoted struct {
		ValidatedRule domain.ValidatedRule `json:"validated_rule"`
		SkillVersion  domain.SkillVersion  `json:"skill_version"`
	}
	adminPost(t, server.URL+"/admin/candidates/"+provision.Tenant.ID+"/"+listed.Candidates[0].ID+"/promote", map[string]any{
		"reviewer_id": "reviewer:alice",
		"rationale":   "three matching corrections",
	}, &promoted, http.StatusCreated)
	if len(promoted.SkillVersion.RuleManifest) != 1 {
		t.Fatalf("manifest count = %d, want 1", len(promoted.SkillVersion.RuleManifest))
	}

	var after struct {
		CaseRun      domain.CaseRun      `json:"case_run"`
		ReviewPacket domain.ReviewPacket `json:"review_packet"`
	}
	post(t, server.URL+"/cases", provision.Tenant.ID, map[string]any{
		"workflow_slug": "quote_drafting",
		"mode":          "sandbox",
		"payload": map[string]any{
			"sandbox":         true,
			"case_name":       "after",
			"client_type":     "new",
			"photos_complete": false,
		},
	}, &after, http.StatusCreated)
	if after.ReviewPacket.PlannedAction.Type != "hold_and_request_more_info" {
		t.Fatalf("planned action = %s", after.ReviewPacket.PlannedAction.Type)
	}

	var tools struct {
		Tools []string `json:"tools"`
	}
	get(t, server.URL+"/mcp/tools?mode=sandbox", "", &tools, http.StatusOK)
	for _, tool := range tools.Tools {
		if tool == "send_draft_email" {
			t.Fatal("sandbox tools exposed send_draft_email")
		}
	}
}

func TestHTTPTenantContextComesFromAuthToken(t *testing.T) {
	application := app.New(memory.New())
	apiServer := httpapi.New(application, nil)
	apiServer.SetAdminToken(testAdminToken)
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	var tenantA struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	adminPost(t, server.URL+"/admin/tenants", map[string]any{
		"name":            "Tenant A",
		"vertical":        "roofing",
		"provider_number": "+61400000000",
		"operator_id":     "op_telegram:a",
	}, &tenantA, http.StatusCreated)

	var tenantB struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	adminPost(t, server.URL+"/admin/tenants", map[string]any{
		"name":            "Tenant B",
		"vertical":        "roofing",
		"provider_number": "+61400000001",
		"operator_id":     "op_telegram:b",
	}, &tenantB, http.StatusCreated)

	var start struct {
		CaseRun domain.CaseRun `json:"case_run"`
	}
	post(t, server.URL+"/cases", tenantA.Tenant.ID, map[string]any{
		"tenant_id":     tenantB.Tenant.ID,
		"workflow_slug": "quote_drafting",
		"mode":          "sandbox",
		"payload": map[string]any{
			"sandbox":         true,
			"client_type":     "new",
			"photos_complete": false,
		},
	}, nil, http.StatusBadRequest)

	postWithHeaders(t, server.URL+"/cases", map[string]string{
		"Content-Type":            "application/json",
		"Authorization":           "Bearer tid:" + tenantA.Tenant.ID,
		"X-Victoria-Tenant-Id":    tenantB.Tenant.ID,
		"X-Forwarded-Tenant-Id":   tenantB.Tenant.ID,
		"X-Requested-Tenant-Id":   tenantB.Tenant.ID,
		"X-Victoria-Operator-Id":  "op_telegram:b",
		"X-Client-Supplied-Tid":   tenantB.Tenant.ID,
		"X-Another-Forged-Header": tenantB.Tenant.ID,
	}, map[string]any{
		"workflow_slug": "quote_drafting",
		"mode":          "sandbox",
		"payload": map[string]any{
			"sandbox":         true,
			"client_type":     "new",
			"photos_complete": false,
		},
	}, &start, http.StatusCreated)
	if start.CaseRun.TenantID != tenantA.Tenant.ID {
		t.Fatalf("case tenant = %s, want auth tenant %s", start.CaseRun.TenantID, tenantA.Tenant.ID)
	}

	get(t, server.URL+"/candidates", "", nil, http.StatusUnauthorized)
	get(t, server.URL+"/skill-versions/active?workflow_slug=quote_drafting", tenantB.Tenant.ID, nil, http.StatusOK)
}

func TestHTTPMCPWriteFinalBoundTenantFromAuth(t *testing.T) {
	application := app.New(memory.New())
	apiServer := httpapi.New(application, nil)
	apiServer.SetAdminToken(testAdminToken)
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	var tenantA struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	adminPost(t, server.URL+"/admin/tenants", map[string]any{
		"name":            "Tenant A",
		"vertical":        "roofing",
		"provider_number": "+61400000000",
		"operator_id":     "op_telegram:a",
	}, &tenantA, http.StatusCreated)
	var tenantB struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	adminPost(t, server.URL+"/admin/tenants", map[string]any{
		"name":            "Tenant B",
		"vertical":        "roofing",
		"provider_number": "+61400000001",
		"operator_id":     "op_telegram:b",
	}, &tenantB, http.StatusCreated)

	post(t, server.URL+"/mcp/write-final", "", map[string]any{
		"server_mode": "live",
		"request": map[string]any{
			"tenant_header":     tenantA.Tenant.ID,
			"case_run_id":       "cr_x",
			"decision_point_id": "dp_x",
			"mode":              "live",
			"tool_name":         "send_draft_email",
		},
	}, nil, http.StatusUnauthorized)

	post(t, server.URL+"/mcp/write-final", tenantA.Tenant.ID, map[string]any{
		"server_mode": "live",
		"request": map[string]any{
			"tenant_header":     tenantB.Tenant.ID,
			"case_run_id":       "cr_x",
			"decision_point_id": "dp_x",
			"mode":              "live",
			"tool_name":         "send_draft_email",
		},
	}, nil, http.StatusForbidden)
}

func TestHTTPCustomerInboundAndWhatsAppAllowlistAPI(t *testing.T) {
	application := app.New(memory.New())
	apiServer := httpapi.New(application, nil)
	apiServer.SetAdminToken(testAdminToken)
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	var provision struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	adminPost(t, server.URL+"/admin/tenants", map[string]any{
		"name":            "Tenant A",
		"vertical":        "roofing",
		"provider_number": "+61400000000",
		"operator_id":     "op_telegram:a",
	}, &provision, http.StatusCreated)

	var consent struct {
		Binding domain.ChannelBinding `json:"binding"`
	}
	post(t, server.URL+"/channel-bindings/whatsapp/consent", provision.Tenant.ID, map[string]any{
		"inbound_mode":       "read_only",
		"draft_delivery_jid": "85270000000@s.whatsapp.net",
	}, &consent, http.StatusOK)
	if consent.Binding.ConsentAcknowledgedAt == nil {
		t.Fatal("consent timestamp missing")
	}

	var customers struct {
		Customers []string `json:"customers"`
	}
	post(t, server.URL+"/channel-bindings/whatsapp/customers", provision.Tenant.ID, map[string]any{
		"customer": "+85299999999",
	}, &customers, http.StatusOK)
	if len(customers.Customers) != 1 || customers.Customers[0] != "85299999999@s.whatsapp.net" {
		t.Fatalf("customers = %+v", customers.Customers)
	}
	deleteJSON(t, server.URL+"/channel-bindings/whatsapp/customers", provision.Tenant.ID, map[string]any{
		"customer": "85299999999@s.whatsapp.net",
	}, &customers, http.StatusOK)
	if len(customers.Customers) != 0 {
		t.Fatalf("customers after delete = %+v, want empty", customers.Customers)
	}

	var ingest struct {
		CaseRunID string `json:"case_run_id"`
	}
	post(t, server.URL+"/ingest/customer-message", provision.Tenant.ID, map[string]any{
		"channel":             "email",
		"source_message_id":   "m1",
		"customer_identifier": "customer@example.com",
		"received_at":         "2026-05-02T10:00:00Z",
		"subject":             "Need a quote",
		"body_text":           "Can I get a quote?",
	}, &ingest, http.StatusAccepted)
	if ingest.CaseRunID == "" {
		t.Fatal("case_run_id missing from ingestion response")
	}
	var duplicate struct {
		CaseRunID string `json:"case_run_id"`
	}
	post(t, server.URL+"/ingest/customer-message", provision.Tenant.ID, map[string]any{
		"channel":             "email",
		"source_message_id":   "m1",
		"customer_identifier": "customer@example.com",
		"received_at":         "2026-05-02T10:00:00Z",
		"subject":             "Need a quote",
		"body_text":           "Can I get a quote?",
	}, &duplicate, http.StatusAccepted)
	if duplicate.CaseRunID != ingest.CaseRunID {
		t.Fatalf("duplicate case_run_id = %s, want %s", duplicate.CaseRunID, ingest.CaseRunID)
	}
}

func TestHTTPWhatsAppCommandSecretLifecycle(t *testing.T) {
	application := app.New(memory.New())
	apiServer := httpapi.New(application, nil)
	apiServer.SetAdminToken(testAdminToken)
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	var provision struct {
		Tenant   domain.Tenant               `json:"tenant"`
		Manifest domain.ProvisioningManifest `json:"manifest"`
	}
	adminPost(t, server.URL+"/admin/tenants", map[string]any{
		"name":            "Tenant Sec",
		"vertical":        "roofing",
		"provider_number": "+61400000099",
		"operator_id":     "op_telegram:sec",
	}, &provision, http.StatusCreated)
	if provision.Manifest.WhatsAppCommandSecret == "" {
		t.Fatal("provisioning response missing whatsapp_command_secret")
	}
	originalSecret := provision.Manifest.WhatsAppCommandSecret

	// Acknowledge consent — response includes the binding. Secret MUST be redacted.
	var consent struct {
		Binding map[string]any `json:"binding"`
	}
	post(t, server.URL+"/channel-bindings/whatsapp/consent", provision.Tenant.ID, map[string]any{
		"inbound_mode": "full_control",
	}, &consent, http.StatusOK)
	if _, leaked := consent.Binding["command_registration_secret"]; leaked {
		t.Fatalf("consent response leaked command_registration_secret: %+v", consent.Binding)
	}

	// Reissue endpoint returns a fresh, distinct secret.
	var reissue struct {
		WhatsAppCommandSecret string `json:"whatsapp_command_secret"`
	}
	adminPost(t, server.URL+"/admin/channel-bindings/whatsapp/command-secret", map[string]any{
		"tenant_id": provision.Tenant.ID,
	}, &reissue, http.StatusOK)
	if reissue.WhatsAppCommandSecret == "" {
		t.Fatal("reissue response missing whatsapp_command_secret")
	}
	if reissue.WhatsAppCommandSecret == originalSecret {
		t.Fatal("reissued secret matches original; rotation not effective")
	}
}

func TestHTTPGatewayInboundRequiresToken(t *testing.T) {
	application := app.New(memory.New())
	apiServer := httpapi.New(application, nil)
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	// No token configured at all → 503 default-deny (operator-reply webhook closed).
	postWithHeaders(t, server.URL+"/gateway/inbound", map[string]string{
		"Content-Type": "application/json",
	}, map[string]any{}, nil, http.StatusServiceUnavailable)

	// Configure a token; missing/incorrect Authorization → 401.
	apiServer.SetGatewayInboundToken("right-token")
	postWithHeaders(t, server.URL+"/gateway/inbound", map[string]string{
		"Content-Type": "application/json",
	}, map[string]any{}, nil, http.StatusUnauthorized)
	postWithHeaders(t, server.URL+"/gateway/inbound", map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer gw:wrong-token",
	}, map[string]any{}, nil, http.StatusUnauthorized)
}

func TestHTTPAdminRoutesRequireToken(t *testing.T) {
	application := app.New(memory.New())
	apiServer := httpapi.New(application, nil)
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	body := map[string]any{
		"name":            "Tenant",
		"vertical":        "roofing",
		"provider_number": "+61400000000",
		"operator_id":     "op_telegram:x",
	}

	// No admin token configured at all → 503 default-deny (control plane closed).
	postWithHeaders(t, server.URL+"/admin/tenants", map[string]string{
		"Content-Type": "application/json",
	}, body, nil, http.StatusServiceUnavailable)

	// Configure a token; missing or incorrect Authorization → 401.
	apiServer.SetAdminToken("right-admin-token")
	postWithHeaders(t, server.URL+"/admin/tenants", map[string]string{
		"Content-Type": "application/json",
	}, body, nil, http.StatusUnauthorized)
	postWithHeaders(t, server.URL+"/admin/tenants", map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer admin:wrong-token",
	}, body, nil, http.StatusUnauthorized)

	// Correct token → provisions.
	postWithHeaders(t, server.URL+"/admin/tenants", map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer admin:right-admin-token",
	}, body, nil, http.StatusCreated)
}

// testAdminToken is the shared secret the e2e suite configures on every server
// to exercise the authenticated /admin/* control-plane routes.
const testAdminToken = "test-admin-token"

// adminPost calls a privileged /admin/* route with the admin bearer token.
func adminPost(t *testing.T, url string, body any, out any, wantStatus int) {
	t.Helper()
	postWithHeaders(t, url, map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer admin:" + testAdminToken,
	}, body, out, wantStatus)
}

func post(t *testing.T, url string, tenantID string, body any, out any, wantStatus int) {
	t.Helper()
	headers := map[string]string{"Content-Type": "application/json"}
	if tenantID != "" {
		headers["Authorization"] = "Bearer tid:" + tenantID
	}
	postWithHeaders(t, url, headers, body, out, wantStatus)
}

func postWithHeaders(t *testing.T, url string, headers map[string]string, body any, out any, wantStatus int) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != wantStatus {
		var errBody map[string]any
		_ = json.NewDecoder(res.Body).Decode(&errBody)
		t.Fatalf("POST %s status = %d body=%v, want %d", url, res.StatusCode, errBody, wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

func deleteJSON(t *testing.T, url string, tenantID string, body any, out any, wantStatus int) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodDelete, url, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tenantID != "" {
		req.Header.Set("Authorization", "Bearer tid:"+tenantID)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != wantStatus {
		var errBody map[string]any
		_ = json.NewDecoder(res.Body).Decode(&errBody)
		t.Fatalf("DELETE %s status = %d body=%v, want %d", url, res.StatusCode, errBody, wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

func get(t *testing.T, url string, tenantID string, out any, wantStatus int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tenantID != "" {
		req.Header.Set("Authorization", "Bearer tid:"+tenantID)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != wantStatus {
		var errBody map[string]any
		_ = json.NewDecoder(res.Body).Decode(&errBody)
		t.Fatalf("GET %s status = %d body=%v, want %d", url, res.StatusCode, errBody, wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}
