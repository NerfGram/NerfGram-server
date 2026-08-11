package main

import (
	"net/http"
	"strings"

	"telesrv/internal/admin"
)

// Premium catalog reads and writes share the same named right as the internal
// Admin API. The browser-facing process enforces it before proxying, while the
// upstream API checks it again on its bearer token.
func (s *server) premiumManage(handler http.HandlerFunc) http.Handler {
	return s.requireAuthAPI(s.requirePermission(permissionPremiumManage, handler))
}

func (s *server) handlePremiumPlansAPI(w http.ResponseWriter, r *http.Request) {
	s.proxyAdminJSONNoStore(w, r, "/v1/premium/plans", 1<<20)
}

type upsertPremiumPlanAPIRequest struct {
	CommandID       string `json:"command_id"`
	Reason          string `json:"reason"`
	Confirm         bool   `json:"confirm"`
	Months          int    `json:"months"`
	DurationDays    int    `json:"duration_days"`
	AmountStars     int64  `json:"amount_stars"`
	FiatCurrency    string `json:"fiat_currency"`
	FiatAmount      int64  `json:"fiat_amount"`
	StoreProduct    string `json:"store_product"`
	StoreQuantity   int    `json:"store_quantity"`
	Enabled         bool   `json:"enabled"`
	SortOrder       int    `json:"sort_order"`
	Label           string `json:"label"`
	ExpectedVersion int64  `json:"expected_version"`
}

func (s *server) handleUpsertPremiumPlanAPI(w http.ResponseWriter, r *http.Request) {
	var body upsertPremiumPlanAPIRequest
	if !decodeAction(w, r, &body) {
		return
	}
	req := admin.UpsertPremiumPlanRequest{
		CommandMeta: s.commandMetaFromAPI(
			r, body.CommandID, strings.TrimSpace(body.Reason), body.Confirm, "premium-plan",
		),
		Months: body.Months, DurationDays: body.DurationDays, AmountStars: body.AmountStars,
		FiatCurrency: strings.ToUpper(strings.TrimSpace(body.FiatCurrency)), FiatAmount: body.FiatAmount,
		StoreProduct: strings.TrimSpace(body.StoreProduct), StoreQuantity: body.StoreQuantity,
		Enabled: body.Enabled, SortOrder: body.SortOrder, Label: strings.TrimSpace(body.Label),
		ExpectedVersion: body.ExpectedVersion,
	}
	result, err := s.callAdminAPI(r.Context(), "/v1/premium/plans/upsert", req)
	writeCommandResultAPI(w, result, err)
}
