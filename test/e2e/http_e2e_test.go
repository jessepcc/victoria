package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"victoria/internal/app"
	"victoria/internal/domain"
	"victoria/internal/httpapi"
	"victoria/internal/store/memory"
)

func TestHTTPGoldenCorrectionLoop(t *testing.T) {
	application := app.New(memory.New())
	server := httptest.NewServer(httpapi.New(application, nil).Handler())
	t.Cleanup(server.Close)

	var provision struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	post(t, server.URL+"/admin/tenants", "", map[string]any{
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
		post(t, server.URL+"/gateway/inbound", "", map[string]any{
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
	post(t, server.URL+"/admin/candidates/"+provision.Tenant.ID+"/"+listed.Candidates[0].ID+"/promote", "", map[string]any{
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
	server := httptest.NewServer(httpapi.New(application, nil).Handler())
	t.Cleanup(server.Close)

	var tenantA struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	post(t, server.URL+"/admin/tenants", "", map[string]any{
		"name":            "Tenant A",
		"vertical":        "roofing",
		"provider_number": "+61400000000",
		"operator_id":     "op_telegram:a",
	}, &tenantA, http.StatusCreated)

	var tenantB struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	post(t, server.URL+"/admin/tenants", "", map[string]any{
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
	server := httptest.NewServer(httpapi.New(application, nil).Handler())
	t.Cleanup(server.Close)

	var tenantA struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	post(t, server.URL+"/admin/tenants", "", map[string]any{
		"name":            "Tenant A",
		"vertical":        "roofing",
		"provider_number": "+61400000000",
		"operator_id":     "op_telegram:a",
	}, &tenantA, http.StatusCreated)
	var tenantB struct {
		Tenant domain.Tenant `json:"tenant"`
	}
	post(t, server.URL+"/admin/tenants", "", map[string]any{
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
