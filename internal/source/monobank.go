package source

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/model"
)

var monobankBaseURL = "https://api.monobank.ua"

// rateCacheCurrencies is the set of ISO 4217 numeric codes we cache rates for.
var rateCacheCurrencies = map[int]bool{
	980: true, // UAH
	840: true, // USD
	978: true, // EUR
	756: true, // CHF
}

type monobankRateEntry struct {
	CurrencyCodeA int     `json:"currencyCodeA"`
	CurrencyCodeB int     `json:"currencyCodeB"`
	RateBuy       float64 `json:"rateBuy"`
	RateSell      float64 `json:"rateSell"`
	RateCross     float64 `json:"rateCross"`
}

// FetchRates fetches current exchange rates from Monobank's public API and
// returns a map keyed by "ALPHA_A/ALPHA_B" (e.g. "USD/UAH"), filtered to
// rateCacheCurrencies. The rate value means "1 ALPHA_A = rate ALPHA_B".
// Uses rateCross when non-zero, otherwise the average of rateBuy and rateSell.
func FetchRates() (map[string]decimal.Decimal, error) {
	resp, err := http.Get(monobankBaseURL + "/bank/currency") //nolint:noctx
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

	var entries []monobankRateEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}

	result := make(map[string]decimal.Decimal)
	for _, e := range entries {
		if !rateCacheCurrencies[e.CurrencyCodeA] || !rateCacheCurrencies[e.CurrencyCodeB] {
			continue
		}
		alphaA, ok := currencyAlpha[e.CurrencyCodeA]
		if !ok {
			continue
		}
		alphaB, ok := currencyAlpha[e.CurrencyCodeB]
		if !ok {
			continue
		}

		var rate decimal.Decimal
		if e.RateCross != 0 {
			rate = decimal.NewFromFloat(e.RateCross)
		} else {
			rate = decimal.NewFromFloat(e.RateBuy).Add(decimal.NewFromFloat(e.RateSell)).Div(decimal.NewFromInt(2))
		}
		result[alphaA+"/"+alphaB] = rate
	}
	return result, nil
}

// currencyAlpha maps ISO 4217 numeric codes to alpha codes.
var currencyAlpha = map[int]string{
	980: "UAH", 840: "USD", 978: "EUR", 826: "GBP",
	985: "PLN", 756: "CHF", 392: "JPY", 156: "CNY",
	643: "RUB", 946: "RON", 348: "HUF", 203: "CZK",
	208: "DKK", 578: "NOK", 752: "SEK",
}

// currencyExp maps ISO 4217 numeric codes that are NOT 2-decimal to their exponent.
var currencyExp = map[int]int{
	392: 0, // JPY
}

type monobankClientInfo struct {
	Accounts []monobankAccount `json:"accounts"`
}

type monobankAccount struct {
	ID           string   `json:"id"`
	CurrencyCode int      `json:"currencyCode"`
	MaskedPan    []string `json:"maskedPan"`
	IBAN         string   `json:"iban"`
}

type monobankTxn struct {
	ID              string `json:"id"`
	Time            int64  `json:"time"`
	Description     string `json:"description"`
	MCC             int    `json:"mcc"`
	Amount          int64  `json:"amount"`
	OperationAmount int64  `json:"operationAmount"`
	CurrencyCode    int    `json:"currencyCode"`
	Comment         string `json:"comment"`
}

type monobankSource struct {
	token      string
	accountIDs map[string]struct{}
	from       time.Time
	to         time.Time
}

func NewMonobank(token string, accountIDs []string, since time.Time) Source {
	ids := make(map[string]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		ids[id] = struct{}{}
	}
	now := time.Now()
	from := now.AddDate(0, 0, -31)
	if !since.IsZero() {
		from = since
	}
	return &monobankSource{
		token:      token,
		accountIDs: ids,
		from:       from,
		to:         now,
	}
}

func (s *monobankSource) Transactions() ([]model.Transaction, error) {
	if s.token == "" {
		return nil, fmt.Errorf("monobank token is not configured; set sources.monobank.token in config")
	}

	info, err := s.fetchClientInfo()
	if err != nil {
		return nil, fmt.Errorf("monobank client info: %w", err)
	}

	var txns []model.Transaction
	for _, acc := range info.Accounts {
		id := monobankAccountID(acc)
		if len(s.accountIDs) > 0 {
			if _, ok := s.accountIDs[id]; !ok {
				log.Printf("skipping Monobank account %s (no mapping configured)", id)
				continue
			}
		}
		log.Printf("fetching Monobank statement for %s", id)
		accTxns, err := s.fetchStatement(acc)
		if err != nil {
			return nil, fmt.Errorf("monobank statement for %s: %w", id, err)
		}
		txns = append(txns, accTxns...)
	}
	return txns, nil
}

func (s *monobankSource) fetchClientInfo() (*monobankClientInfo, error) {
	body, err := s.get("/personal/client-info")
	if err != nil {
		return nil, err
	}
	var info monobankClientInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (s *monobankSource) fetchStatement(acc monobankAccount) ([]model.Transaction, error) {
	path := fmt.Sprintf("/personal/statement/%s/%d/%d",
		acc.ID, s.from.Unix(), s.to.Unix())
	body, err := s.get(path)
	if err != nil {
		return nil, err
	}

	var raw []monobankTxn
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	alpha, ok := currencyAlpha[acc.CurrencyCode]
	if !ok {
		alpha = strconv.Itoa(acc.CurrencyCode)
	}
	exp := 2
	if e, ok := currencyExp[acc.CurrencyCode]; ok {
		exp = e
	}
	divisor := decimal.New(1, int32(exp))
	accID := monobankAccountID(acc)

	txns := make([]model.Transaction, 0, len(raw))
	for _, r := range raw {
		desc := r.Description
		if r.Comment != "" {
			desc = r.Comment
		}
		mccStr := strconv.Itoa(r.MCC)

		// Resolve operation currency and amount for foreign-currency transactions.
		// When the transaction's currency code differs from the account's, the
		// operation was performed in a different currency and GnuCash needs both
		// the home-currency amount (Amount) and the foreign amount (OperationAmount).
		opAlpha := ""
		opAmount := decimal.Zero
		if r.CurrencyCode != 0 && r.CurrencyCode != acc.CurrencyCode {
			opExp := 2
			if e, ok := currencyExp[r.CurrencyCode]; ok {
				opExp = e
			}
			opDivisor := decimal.New(1, int32(opExp))
			opAmount = decimal.NewFromInt(r.OperationAmount).Div(opDivisor)
			if a, ok := currencyAlpha[r.CurrencyCode]; ok {
				opAlpha = a
			} else {
				opAlpha = strconv.Itoa(r.CurrencyCode)
			}
		}

		txns = append(txns, model.Transaction{
			ID:                r.ID,
			Date:              time.Unix(r.Time, 0),
			Description:       desc,
			Amount:            decimal.NewFromInt(r.Amount).Div(divisor),
			Currency:          alpha,
			OperationAmount:   opAmount,
			OperationCurrency: opAlpha,
			AccountID:         accID,
			Category:          mccStr,
			CategoryLabel:     mccDescription(r.MCC),
		})
	}
	return txns, nil
}

func (s *monobankSource) get(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, monobankBaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Token", s.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := 60 * time.Second
		if v := resp.Header.Get("Retry-After"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil {
				retryAfter = time.Duration(secs) * time.Second
			}
		}
		log.Printf("Monobank rate limit exceeded, waiting %s...", retryAfter)
		time.Sleep(retryAfter)
		return s.get(path)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

func monobankAccountID(acc monobankAccount) string {
	if acc.IBAN != "" {
		return acc.IBAN
	}
	return acc.ID
}
