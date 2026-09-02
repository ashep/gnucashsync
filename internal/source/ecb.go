package source

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var ecbBaseURL = "https://api.frankfurter.dev"

// ecbBase is the currency ECB reference rates are quoted against.
const ecbBase = "EUR"

type ecbResponse struct {
	Date  string                 `json:"date"`
	Rates map[string]json.Number `json:"rates"`
}

// ecbSymbols returns the non-euro currencies to ask ECB about, in a stable
// order. The hryvnia is excluded: ECB does not publish it.
func ecbSymbols() []string {
	var out []string
	for code := range rateCacheCurrencies {
		alpha, ok := currencyAlpha[code]
		if !ok || alpha == ecbBase || alpha == "UAH" {
			continue
		}
		out = append(out, alpha)
	}
	sort.Strings(out)
	return out
}

// FetchECBRates fetches the European Central Bank's daily reference rates for
// the given date and returns them keyed "FROM/TO" (1 FROM = rate TO). These are
// market reference rates, which is why they are preferred over a central bank's
// administratively set official rate for the pairs ECB covers.
//
// ECB fixes rates on business days only; asking for a weekend or holiday yields
// the preceding business day's fixing, which is the conventional rate to apply
// to a transaction on a day the market was closed.
func FetchECBRates(date time.Time) (map[string]decimal.Decimal, error) {
	symbols := ecbSymbols()
	if len(symbols) == 0 {
		return map[string]decimal.Decimal{}, nil
	}

	url := fmt.Sprintf("%s/v1/%s?base=%s&symbols=%s",
		ecbBaseURL, date.Format(time.DateOnly), ecbBase, strings.Join(symbols, ","))

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

	var parsed ecbResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	// perEUR[X] is how many X one euro buys.
	perEUR := make(map[string]decimal.Decimal, len(parsed.Rates))
	for alpha, raw := range parsed.Rates {
		rate, err := decimal.NewFromString(raw.String())
		if err != nil || rate.IsZero() {
			continue
		}
		perEUR[alpha] = rate
	}

	result := make(map[string]decimal.Decimal, len(perEUR)*2)
	for alpha, rate := range perEUR {
		result[ecbBase+"/"+alpha] = rate
	}
	// Crosses between two non-euro currencies: 1 X = (Y per EUR) / (X per EUR) Y.
	for i, x := range symbols {
		for _, y := range symbols[i+1:] {
			px, xok := perEUR[x]
			py, yok := perEUR[y]
			if xok && yok {
				// Rounded: this lands in a config file people read, and eight
				// decimals is far finer than any currency amount can register.
				result[x+"/"+y] = py.Div(px).Round(8)
			}
		}
	}
	return result, nil
}

// FetchHistoricalRates returns the rates in effect on the given date, drawn from
// the most appropriate source for each pair: ECB market reference rates wherever
// they reach, and the National Bank of Ukraine only for the hryvnia pairs ECB
// does not publish. Their key sets are disjoint, so neither can overwrite the
// other.
//
// A failure of one source is not fatal: whatever the other one knows is still
// returned, and an empty result means neither had rates for that date.
func FetchHistoricalRates(date time.Time) (map[string]decimal.Decimal, error) {
	result := make(map[string]decimal.Decimal)

	nbu, nbuErr := FetchNBURates(date)
	for pair, rate := range nbu {
		result[pair] = rate
	}

	ecb, ecbErr := FetchECBRates(date)
	for pair, rate := range ecb {
		result[pair] = rate
	}

	if len(result) == 0 && (nbuErr != nil || ecbErr != nil) {
		return nil, fmt.Errorf("no exchange rates for %s (NBU: %v, ECB: %v)",
			date.Format(time.DateOnly), nbuErr, ecbErr)
	}
	return result, nil
}
