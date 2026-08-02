package loadharness

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const maxServerMetricsBytes = 4 << 20

var selectedServerMetrics = map[string]struct{}{
	"telesrv_mtproto_raw_connections":                       {},
	"telesrv_mtproto_sessions":                              {},
	"telesrv_mtproto_logical_sessions":                      {},
	"telesrv_mtproto_logical_outbox_frames":                 {},
	"telesrv_mtproto_logical_outbox_bytes":                  {},
	"telesrv_mtproto_logical_outbox_acked_frames_total":     {},
	"telesrv_mtproto_logical_outbox_acked_bytes_total":      {},
	"telesrv_mtproto_logical_outbox_retained_seconds_count": {},
	"telesrv_mtproto_logical_outbox_retained_seconds_sum":   {},
	"telesrv_mtproto_pending_push_bytes":                    {},
	"telesrv_mtproto_inbound_rpc_tasks":                     {},
	"telesrv_mtproto_inbound_rpc_bytes":                     {},
	"telesrv_mtproto_inbound_frame_bytes":                   {},
	"telesrv_mtproto_outbound_tracked_bytes":                {},
	"telesrv_mtproto_outbound_write_bytes":                  {},
	"telesrv_mtproto_rpc_execution_owners":                  {},
	"telesrv_mtproto_rpc_execution_reserved_entries":        {},
	"telesrv_mtproto_rpc_execution_receipts":                {},
	"telesrv_mtproto_rpc_execution_receipt_budget_bytes":    {},
	"telesrv_mtproto_rpc_execution_subscribers":             {},
	"telesrv_mtproto_rpc_result_inner_bytes_total":          {},
	"telesrv_mtproto_rpc_result_wire_bytes_total":           {},
	"telesrv_mtproto_rpc_result_delivered_bytes_total":      {},
	"telesrv_go_goroutines":                                 {},
	"telesrv_go_heap_alloc_bytes":                           {},
	"telesrv_go_heap_inuse_bytes":                           {},
	"telesrv_go_heap_objects":                               {},
	"telesrv_go_sys_bytes":                                  {},
	"telesrv_postgres_pool_connections":                     {},
	"telesrv_postgres_pool_acquire_wait_seconds":            {},
	"telesrv_postgres_pool_empty_acquire_count":             {},
	"telesrv_postgres_pool_canceled_acquire_count":          {},
	"telesrv_redis_pool_connections":                        {},
	"telesrv_redis_pool_pending_requests":                   {},
	"telesrv_redis_pool_timeouts":                           {},
	"telesrv_redis_pool_wait_seconds":                       {},
	"telesrv_metrics_dropped_observations_total":            {},
}

type serverMetricsClient struct {
	url     string
	client  *http.Client
	success atomic.Uint64
	errors  atomic.Uint64
}

func newServerMetricsClient(url string) *serverMetricsClient {
	if strings.TrimSpace(url) == "" {
		return nil
	}
	return &serverMetricsClient{url: url, client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *serverMetricsClient) scrape(ctx context.Context) (map[string]float64, error) {
	if c == nil {
		return nil, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		c.errors.Add(1)
		return nil, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		c.errors.Add(1)
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		c.errors.Add(1)
		return nil, fmt.Errorf("metrics HTTP status %d", response.StatusCode)
	}
	reader := bufio.NewScanner(io.LimitReader(response.Body, maxServerMetricsBytes))
	reader.Buffer(make([]byte, 64<<10), 1<<20)
	values := make(map[string]float64, len(selectedServerMetrics))
	for reader.Scan() {
		line := strings.TrimSpace(reader.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if idx := strings.IndexByte(name, '{'); idx >= 0 {
			name = name[:idx]
		}
		if _, ok := selectedServerMetrics[name]; !ok {
			continue
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		// Reports need bounded, comparable capacity signals, not an unbounded copy
		// of Prometheus label series. Aggregate every selected family into one
		// key so method/encoding cardinality can never starve later gauges (the
		// endpoint orders counters before gauges). The source /metrics endpoint
		// retains full labels for detailed diagnosis.
		values[name] += value
	}
	if err := reader.Err(); err != nil {
		c.errors.Add(1)
		return nil, err
	}
	c.success.Add(1)
	return values, nil
}

func (c *serverMetricsClient) successes() uint64 {
	if c == nil {
		return 0
	}
	return c.success.Load()
}

func (c *serverMetricsClient) failures() uint64 {
	if c == nil {
		return 0
	}
	return c.errors.Load()
}
