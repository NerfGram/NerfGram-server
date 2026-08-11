package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"telesrv/internal/admin"
)

func TestPremiumPlanPanelProxyAndPermission(t *testing.T) {
	var upsert admin.UpsertPremiumPlanRequest
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer premium-secret" {
			t.Fatalf("upstream authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/premium/plans":
			_, _ = w.Write([]byte(`{"plans":[{"Months":3,"AmountStars":750,"Version":1}]}`))
		case "/v1/premium/plans/upsert":
			if err := json.NewDecoder(r.Body).Decode(&upsert); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(admin.CommandResult{
				CommandID: upsert.CommandID, Action: admin.ActionUpsertPremiumPlan,
				Status: "completed", DryRun: upsert.DryRun,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	srv := panelServer(t, permissionPremiumManage)
	srv.cfg.AdminAPIURL = upstream.URL
	srv.cfg.AdminAPIToken = "premium-secret"
	cookies, csrf := signIn(t, srv)

	read := withCookies(httptest.NewRequest(http.MethodGet, "/api/premium/plans", nil), cookies)
	readResponse := httptest.NewRecorder()
	srv.routes().ServeHTTP(readResponse, read)
	if readResponse.Code != http.StatusOK ||
		!strings.Contains(readResponse.Body.String(), `"AmountStars":750`) ||
		readResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("read status=%d cache=%q body=%s", readResponse.Code,
			readResponse.Header().Get("Cache-Control"), readResponse.Body.String())
	}

	write := withCookies(httptest.NewRequest(
		http.MethodPost,
		"/api/actions/upsert-premium-plan",
		strings.NewReader(`{"reason":"price review","confirm":false,"months":3,"duration_days":90,"amount_stars":800,"enabled":true,"sort_order":10,"label":"Quarter","expected_version":1}`),
	), cookies)
	write.Header.Set(csrfHeaderName, csrf)
	writeResponse := httptest.NewRecorder()
	srv.routes().ServeHTTP(writeResponse, write)
	if writeResponse.Code != http.StatusOK || upsert.Actor != "admin" || !upsert.DryRun ||
		upsert.Months != 3 || upsert.AmountStars != 800 || upsert.ExpectedVersion != 1 ||
		upsert.CommandID == "" {
		t.Fatalf("write status=%d request=%+v body=%s", writeResponse.Code, upsert, writeResponse.Body.String())
	}

	forbidden := panelServer(t, permissionVerificationReview)
	forbiddenCookies, _ := signIn(t, forbidden)
	forbiddenResponse := httptest.NewRecorder()
	forbidden.routes().ServeHTTP(forbiddenResponse,
		withCookies(httptest.NewRequest(http.MethodGet, "/api/premium/plans", nil), forbiddenCookies))
	if forbiddenResponse.Code != http.StatusForbidden ||
		!strings.Contains(forbiddenResponse.Body.String(), permissionPremiumManage) {
		t.Fatalf("forbidden status=%d body=%s", forbiddenResponse.Code, forbiddenResponse.Body.String())
	}
}
