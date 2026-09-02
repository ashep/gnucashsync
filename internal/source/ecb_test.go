package source_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/source"
)

const ecbBody = `{"amount":1.0,"base":"EUR","date":"2026-08-27","rates":{"CHF":0.9376,"USD":1.1645}}`

func TestFetchECBRates(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query().Get("base")
		w.Write([]byte(ecbBody))
	}))
	defer srv.Close()
	source.SetECBBaseURL(srv.URL)

	rates, err := source.FetchECBRates(time.Date(2026, 8, 27, 15, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	if want := "/v1/2026-08-27"; gotPath != want {
		t.Errorf("path: got %q, want %q", gotPath, want)
	}
	if gotQuery != "EUR" {
		t.Errorf("base: got %q, want EUR", gotQuery)
	}

	if got, want := rates["EUR/USD"], decimal.RequireFromString("1.1645"); !got.Equal(want) {
		t.Errorf("EUR/USD: got %s, want %s", got, want)
	}
	if got, want := rates["EUR/CHF"], decimal.RequireFromString("0.9376"); !got.Equal(want) {
		t.Errorf("EUR/CHF: got %s, want %s", got, want)
	}
	// Crosses between non-euro currencies are derived: 1 CHF = (USD per EUR) ÷
	// (CHF per EUR) USD = 1.1645 / 0.9376.
	if got, want := rates["CHF/USD"], decimal.RequireFromString("1.24200085"); !got.Equal(want) {
		t.Errorf("CHF/USD: got %s, want %s", got, want)
	}
	// The hryvnia is not an ECB currency and must never appear.
	for pair := range rates {
		if pair == "USD/UAH" || pair == "EUR/UAH" {
			t.Errorf("ECB must not report UAH pairs, got %q", pair)
		}
	}
}

// TestFetchHistoricalRates_PrefersECBForNonHryvniaPairs verifies the merge rule:
// ECB market rates win for the pairs it covers, and NBU supplies only the
// hryvnia pairs ECB does not publish.
func TestFetchHistoricalRates_PrefersECBForNonHryvniaPairs(t *testing.T) {
	ecb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(ecbBody))
	}))
	defer ecb.Close()
	nbu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(nbuBody))
	}))
	defer nbu.Close()
	source.SetECBBaseURL(ecb.URL)
	source.SetNBUBaseURL(nbu.URL)

	rates, err := source.FetchHistoricalRates(time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	// ECB's market rate, not NBU's cross of 52.008/44.5717 = 1.16684.
	if got, want := rates["EUR/USD"], decimal.RequireFromString("1.1645"); !got.Equal(want) {
		t.Errorf("EUR/USD should come from ECB: got %s, want %s", got, want)
	}
	// NBU covers what ECB cannot.
	if got, want := rates["EUR/UAH"], decimal.RequireFromString("52.008"); !got.Equal(want) {
		t.Errorf("EUR/UAH should come from NBU: got %s, want %s", got, want)
	}
	if got, want := rates["USD/UAH"], decimal.RequireFromString("44.5717"); !got.Equal(want) {
		t.Errorf("USD/UAH should come from NBU: got %s, want %s", got, want)
	}
}

// TestFetchHistoricalRates_ToleratesOneSourceFailing verifies that losing one
// rate service still yields whatever the other one knows.
func TestFetchHistoricalRates_ToleratesOneSourceFailing(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down.Close()
	nbu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(nbuBody))
	}))
	defer nbu.Close()
	source.SetECBBaseURL(down.URL)
	source.SetNBUBaseURL(nbu.URL)

	rates, err := source.FetchHistoricalRates(time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("one source failing should not fail the lookup: %v", err)
	}
	if _, ok := rates["EUR/UAH"]; !ok {
		t.Errorf("expected NBU rates to survive an ECB outage, got %v", rates)
	}
}
