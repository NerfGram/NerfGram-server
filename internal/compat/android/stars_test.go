package android

import (
	"encoding/json"
	"testing"
)

func TestDirectInvoiceCurrenciesAreStableAndContainUSD(t *testing.T) {
	values := DirectInvoiceCurrencies()
	if len(values) == 0 {
		t.Fatal("direct invoice currency list is empty")
	}
	foundUSD := false
	for _, value := range values {
		if value == "USD" {
			foundUSD = true
		}
	}
	if !foundUSD {
		t.Fatalf("direct invoice currencies = %v, want USD", values)
	}
	values[0] = "MUTATED"
	if DirectInvoiceCurrencies()[0] == "MUTATED" {
		t.Fatal("DirectInvoiceCurrencies returned mutable package storage")
	}
	var decoded []string
	if err := json.Unmarshal([]byte(DirectInvoiceCurrenciesJSON()), &decoded); err != nil || len(decoded) != len(values) {
		t.Fatalf("currency JSON = %q decoded=%v err=%v", DirectInvoiceCurrenciesJSON(), decoded, err)
	}
}
