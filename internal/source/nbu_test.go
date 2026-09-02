package source_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/source"
)

const nbuBody = `[
{"r030":36,"txt":"Австралійський долар","rate":29.1,"cc":"AUD","exchangedate":"27.08.2026"},
{"r030":756,"txt":"Швейцарський франк","rate":55.434,"cc":"CHF","exchangedate":"27.08.2026"},
{"r030":840,"txt":"Долар США","rate":44.5717,"cc":"USD","exchangedate":"27.08.2026"},
{"r030":978,"txt":"Євро","rate":52.008,"cc":"EUR","exchangedate":"27.08.2026"}
]`

func TestFetchNBURates(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(nbuBody))
	}))
	defer srv.Close()
	source.SetNBUBaseURL(srv.URL)

	rates, err := source.FetchNBURates(time.Date(2026, 8, 27, 15, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	if want := "date=20260827&json"; gotQuery != want {
		t.Errorf("query: got %q, want %q", gotQuery, want)
	}
	// AUD is outside the cached currency set and must be dropped.
	if len(rates) != 3 {
		t.Fatalf("expected 3 rates, got %d: %v", len(rates), rates)
	}
	if got, want := rates["USD/UAH"], decimal.RequireFromString("44.5717"); !got.Equal(want) {
		t.Errorf("USD/UAH: got %s, want %s", got, want)
	}
	if got, want := rates["EUR/UAH"], decimal.RequireFromString("52.008"); !got.Equal(want) {
		t.Errorf("EUR/UAH: got %s, want %s", got, want)
	}
}

// TestFetchNBURates_FutureDate verifies that a date NBU has not published is
// reported as "no rates" rather than an error: it is an expected state.
func TestFetchNBURates_FutureDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	source.SetNBUBaseURL(srv.URL)

	rates, err := source.FetchNBURates(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("empty response should not be an error: %v", err)
	}
	if len(rates) != 0 {
		t.Errorf("expected no rates, got %v", rates)
	}
}
