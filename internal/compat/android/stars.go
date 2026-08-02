package android

import "encoding/json"

// DirectInvoiceCurrencies are the Google Play billing currencies for which
// DrKLO's official client must use Telegram's invoice flow. telesrv does not
// publish or verify Google Play products, so this prevents Stars options with
// no store_product from entering the Play Billing branch once it is ready.
var directInvoiceCurrencies = []string{
	"AED", "AUD", "BRL", "CAD", "CHF", "CLP", "CNY", "COP", "CZK", "DKK",
	"EGP", "EUR", "GBP", "HKD", "HUF", "IDR", "ILS", "INR", "JPY", "KRW",
	"KZT", "MXN", "MYR", "NGN", "NOK", "NZD", "PEN", "PHP", "PKR", "PLN",
	"QAR", "RON", "RUB", "SAR", "SEK", "SGD", "THB", "TRY", "TWD", "UAH",
	"USD", "VND", "ZAR",
}

func DirectInvoiceCurrencies() []string {
	return append([]string(nil), directInvoiceCurrencies...)
}

func DirectInvoiceCurrenciesJSON() string {
	body, _ := json.Marshal(directInvoiceCurrencies)
	return string(body)
}
