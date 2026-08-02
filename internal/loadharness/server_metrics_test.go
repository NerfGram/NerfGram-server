package loadharness

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServerMetricsScrapeSelectsBoundedCapacitySignals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `telesrv_mtproto_raw_connections 500`)
		fmt.Fprintln(w, `telesrv_mtproto_sessions{state="active"} 499`)
		fmt.Fprintln(w, `telesrv_mtproto_sessions{state="provisional"} 1`)
		for i := 0; i < 256; i++ {
			fmt.Fprintf(w, "telesrv_mtproto_rpc_result_wire_bytes_total{method=%q} 1\n", fmt.Sprintf("method-%d", i))
		}
		fmt.Fprintln(w, `unrelated_high_cardinality{user_id="secret"} 1`)
	}))
	defer server.Close()
	client := newServerMetricsClient(server.URL)
	values, err := client.scrape(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if values["telesrv_mtproto_raw_connections"] != 500 || values["telesrv_mtproto_sessions"] != 500 || values["telesrv_mtproto_rpc_result_wire_bytes_total"] != 256 {
		t.Fatalf("values = %#v", values)
	}
	if len(values) != 3 || client.successes() != 1 || client.failures() != 0 {
		t.Fatalf("bounded values/scrapes = %#v, %d/%d", values, client.successes(), client.failures())
	}
}
