package source_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/source"
)

func TestFetchRates_UsesCrossRateWhenAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bank/currency" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"currencyCodeA":840,"currencyCodeB":980,"date":1750000000,"rateBuy":41.0,"rateSell":41.8,"rateCross":0},
			{"currencyCodeA":978,"currencyCodeB":980,"date":1750000000,"rateBuy":0,"rateSell":0,"rateCross":44.5},
			{"currencyCodeA":756,"currencyCodeB":980,"date":1750000000,"rateBuy":46.0,"rateSell":47.0,"rateCross":0},
			{"currencyCodeA":999,"currencyCodeB":980,"date":1750000000,"rateBuy":1.0,"rateSell":2.0,"rateCross":0}
		]`)
	}))
	defer srv.Close()
	source.SetMonobankBaseURL(srv.URL)
	defer source.SetMonobankBaseURL("https://api.monobank.ua")

	rates, err := source.FetchRates()
	if err != nil {
		t.Fatal(err)
	}

	// USD/UAH: no cross rate → average of buy(41.0) and sell(41.8) = 41.4
	got, ok := rates["USD/UAH"]
	if !ok {
		t.Fatal("expected USD/UAH in rates")
	}
	if !got.Equal(decimal.NewFromFloat(41.4)) {
		t.Errorf("USD/UAH: expected 41.4, got %s", got)
	}

	// EUR/UAH: cross rate = 44.5
	got, ok = rates["EUR/UAH"]
	if !ok {
		t.Fatal("expected EUR/UAH in rates")
	}
	if !got.Equal(decimal.NewFromFloat(44.5)) {
		t.Errorf("EUR/UAH: expected 44.5, got %s", got)
	}

	// CHF/UAH: no cross → average of 46.0 and 47.0 = 46.5
	got, ok = rates["CHF/UAH"]
	if !ok {
		t.Fatal("expected CHF/UAH in rates")
	}
	if !got.Equal(decimal.NewFromFloat(46.5)) {
		t.Errorf("CHF/UAH: expected 46.5, got %s", got)
	}

	// Currency code 999 is not in our supported set — must be absent
	if _, ok := rates["999/UAH"]; ok {
		t.Error("unexpected unsupported currency pair in rates")
	}
}
