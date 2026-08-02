package loadharness

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/iamxvbaba/td/pool"
	tdrpc "github.com/iamxvbaba/td/rpc"
)

func TestOperationMetricsUsesBoundedHistogramAndFixedErrorClasses(t *testing.T) {
	metrics := &operationMetrics{}
	metrics.observe(time.Now().Add(-20*time.Millisecond), nil)
	metrics.observe(time.Now().Add(-200*time.Millisecond), errors.New("FLOOD_WAIT_1 phone=secret"))
	report := metrics.report()
	if report.Count != 2 || report.Errors != 1 || report.FloodWaits != 1 {
		t.Fatalf("report = %#v", report)
	}
	if report.P50UpperMS <= 0 || report.P99UpperMS < report.P50UpperMS || report.MaxMS <= 0 {
		t.Fatalf("latency report = %#v", report)
	}
}

func TestClassifyErrorReasonUsesFiniteRedactedVocabulary(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{errors.New("dial tcp 10.0.0.1:2398: socket: too many open files"), "file_descriptor_limit"},
		{errors.New("read: temporary auth key not found: pfs reconnect required"), "pfs_reconnect"},
		{errors.New("read tcp: EOF auth_key_id=secret"), "eof"},
	}
	for _, test := range tests {
		if got := classifyErrorReason(test.err); got != test.want {
			t.Fatalf("classifyErrorReason(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}

func TestClassifyErrorRecognizesTypedReconnectFailures(t *testing.T) {
	tests := []error{
		fmt.Errorf("invoke: %w", tdrpc.ErrEngineClosed),
		fmt.Errorf("acquire: %w", pool.ErrConnDead),
		fmt.Errorf("read: %w", net.ErrClosed),
		errors.New("write: broken pipe"),
	}
	for _, err := range tests {
		if got := classifyError(err); got != "connection" {
			t.Fatalf("classifyError(%v) = %q, want connection", err, got)
		}
	}
}

func TestOperationMetricsSeparatesHarnessCancellation(t *testing.T) {
	metrics := &operationMetrics{}
	metrics.observe(time.Now(), context.Canceled)
	report := metrics.report()
	if report.Count != 1 || report.Canceled != 1 || report.Errors != 0 {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateReportAllowsOnlyConnectionErrorsForExpectedRestart(t *testing.T) {
	report := &RunReport{
		ExpectedSessions: 2, PeakReadySessions: 2, Reconnects: 2,
		SteadySamples: 1, SteadyReadyRatio: 1, MinSteadyReadySessions: 2,
		Operations: map[string]OperationReport{
			"connection.dead": {Count: 2, Errors: 2, ConnectionErrors: 2},
		},
	}
	evaluateReport(report, RunConfig{MinimumReadyRatio: 1, ExpectServerRestart: true})
	if !report.Pass {
		t.Fatalf("report = %#v", report)
	}
	report.Operations["ping"] = OperationReport{Count: 1, Errors: 1}
	report.Failures = nil
	evaluateReport(report, RunConfig{MinimumReadyRatio: 1, ExpectServerRestart: true})
	if report.Pass {
		t.Fatalf("unexpected application error passed: %#v", report)
	}
}

func TestEvaluateReportRequiresReclamationAndNoFloodWait(t *testing.T) {
	report := &RunReport{
		ExpectedSessions: 10, PeakReadySessions: 10, ServerMetricsScrapes: 1,
		SteadySamples: 1, SteadyReadyRatio: 1, MinSteadyReadySessions: 10,
		Operations: map[string]OperationReport{"ping": {Count: 10}},
		BaselineServerMetrics: map[string]float64{
			"telesrv_mtproto_raw_connections":      2,
			"telesrv_mtproto_logical_outbox_bytes": 3,
		},
		FinalServerMetrics: map[string]float64{
			"telesrv_mtproto_raw_connections":      2,
			"telesrv_mtproto_logical_outbox_bytes": 4,
		},
	}
	evaluateReport(report, RunConfig{MinimumReadyRatio: 1, RecoveryDuration: time.Minute, ServerMetricsURL: "http://metrics"})
	if report.Pass || len(report.Failures) != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestEvaluateReportAcceptsReturnToNonZeroSharedServerBaseline(t *testing.T) {
	report := &RunReport{
		ExpectedSessions: 10, PeakReadySessions: 10, ServerMetricsScrapes: 2,
		SteadySamples: 1, SteadyReadyRatio: 1, MinSteadyReadySessions: 10,
		Operations: map[string]OperationReport{"ping": {Count: 10}},
		BaselineServerMetrics: map[string]float64{
			"telesrv_mtproto_raw_connections":      2,
			"telesrv_mtproto_logical_sessions":     2,
			"telesrv_mtproto_logical_outbox_bytes": 1024,
		},
		FinalServerMetrics: map[string]float64{
			"telesrv_mtproto_raw_connections":      2,
			"telesrv_mtproto_logical_sessions":     2,
			"telesrv_mtproto_logical_outbox_bytes": 1024,
		},
	}
	evaluateReport(report, RunConfig{MinimumReadyRatio: 1, RecoveryDuration: time.Minute, ServerMetricsURL: "http://metrics"})
	if !report.Pass || len(report.Failures) != 0 {
		t.Fatalf("report = %#v", report)
	}
}
