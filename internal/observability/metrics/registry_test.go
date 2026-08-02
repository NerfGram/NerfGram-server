package metrics

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"telesrv/internal/mtprotoedge"
	"telesrv/internal/rpc"
)

var (
	_ mtprotoedge.Metrics                 = (*Registry)(nil)
	_ mtprotoedge.RPCResultMetrics        = (*Registry)(nil)
	_ mtprotoedge.LogicalOutboxMetrics    = (*Registry)(nil)
	_ mtprotoedge.ConnectionIntakeMetrics = (*Registry)(nil)
	_ rpc.Metrics                         = (*Registry)(nil)
)

func TestRegistryExportsBoundedAggregateMetrics(t *testing.T) {
	registry := New()
	registry.maxSeries = 2
	registry.RPCHandled("help.getConfig", 5*time.Millisecond, nil)
	registry.RPCHandled("users.getUsers", time.Second, errors.New("secret auth_key_id=deadbeef session=123"))

	recorder := httptest.NewRecorder()
	registry.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()
	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(body, "telesrv_mtproto_rpc_handled_total") || !strings.Contains(body, "telesrv_mtproto_rpc_duration_seconds_bucket") {
		t.Fatalf("expected RPC counter and histogram, got:\n%s", body)
	}
	if strings.Contains(body, "deadbeef") || strings.Contains(body, "session=123") {
		t.Fatalf("raw error identity leaked into metrics:\n%s", body)
	}
	if got := registry.series.Load(); got != registry.maxSeries {
		t.Fatalf("resident dynamic series = %d, want cap %d", got, registry.maxSeries)
	}
	if got := registry.dropped.Load(); got == 0 {
		t.Fatal("series overflow was not reported")
	}
	if !strings.Contains(body, "telesrv_metrics_dropped_observations_total 2") {
		t.Fatalf("overflow counter missing from:\n%s", body)
	}
}

func TestRegistrySanitizesAndBoundsProviderSamples(t *testing.T) {
	registry := New()
	registry.AddGaugeProvider(func() []GaugeSample {
		return []GaugeSample{{
			Name:   "9 invalid metric",
			Labels: []Label{{Name: "bad:label", Value: "quoted\"\nvalue"}},
			Value:  3,
		}}
	})
	recorder := httptest.NewRecorder()
	registry.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `__invalid_metric{bad_label="quoted\"\nvalue"} 3`) {
		t.Fatalf("provider sample was not safely sanitized:\n%s", body)
	}
}

func TestErrorOutcomeHasFixedCardinality(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, "ok"},
		{errors.New("FLOOD_WAIT_1 for phone 123"), "flood_wait"},
		{errors.New("global capacity exceeded for auth key"), "edge_overload"},
		{errors.New("arbitrary user-controlled failure"), "error"},
	}
	for _, test := range tests {
		if got := errorOutcome(test.err); got != test.want {
			t.Errorf("errorOutcome(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}
