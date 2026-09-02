package source

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

var nbuBaseURL = "https://bank.gov.ua"

type nbuRateEntry struct {
	R030 int         `json:"r030"`
	CC   string      `json:"cc"`
	Rate json.Number `json:"rate"`
}

// FetchNBURates fetches the National Bank of Ukraine's official rates for the
// given date and returns them keyed "ALPHA/UAH" (1 ALPHA = rate UAH), filtered
// to rateCacheCurrencies. NBU quotes every currency against the hryvnia, so
// cross rates such as EUR/USD are derived from these by the caller.
//
// A rate is published for every calendar day, weekends included. Dates NBU has
// not published yet yield an empty result and no error: that is an expected
// state (a transaction imported before the day's rate is set), not a failure.
func FetchNBURates(date time.Time) (map[string]decimal.Decimal, error) {
	url := fmt.Sprintf("%s/NBUStatService/v1/statdirectory/exchange?date=%s&json",
		nbuBaseURL, date.Format("20060102"))

	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	var entries []nbuRateEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}

	result := make(map[string]decimal.Decimal, len(entries))
	for _, e := range entries {
		if !rateCacheCurrencies[e.R030] {
			continue
		}
		alpha, ok := currencyAlpha[e.R030]
		if !ok {
			continue
		}
		rate, err := decimal.NewFromString(e.Rate.String())
		if err != nil || rate.IsZero() {
			continue
		}
		result[alpha+"/UAH"] = rate
	}
	return result, nil
}
