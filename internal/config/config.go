package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"
)

// DefaultCurrencyCacheTTL is how long cached exchange rates remain valid.
const DefaultCurrencyCacheTTL = 24 * time.Hour

const SkipAccount = "SKIP"

type MonobankSource struct {
	Token string `yaml:"token"`
}

type PrivatbankSource struct {
	Dir string `yaml:"dir"`
}

type Sources struct {
	Monobank   MonobankSource   `yaml:"monobank"`
	Privatbank PrivatbankSource `yaml:"privatbank"`
}

type DescriptionRule struct {
	Pattern        string `yaml:"pattern"`
	Amount         string `yaml:"amount,omitempty"`
	NewDescription string `yaml:"new_description,omitempty"`
	Account        string `yaml:"account"`
	re             *regexp.Regexp
	amount         *decimal.Decimal
}

type AccountEntry struct {
	SourceID         string            `yaml:"source_id"`
	Alias            string            `yaml:"alias,omitempty"`
	GnuCashAccount   string            `yaml:"gnucash_account"`
	DescriptionRules []DescriptionRule `yaml:"description_rules"`
	MCCRules         map[string]string `yaml:"mcc_rules"`
}

// ResolveCounterpart returns the counterpart GnuCash account for a transaction.
// It checks description_rules first (first match wins), then per-account mcc_rules,
// then falls back to globalMCCRules (may be nil).
// Description rules may optionally require an exact amount match in addition to the pattern.
// When a description rule sets new_description, it is returned as the second value.
func (e *AccountEntry) ResolveCounterpart(description, category string, amount decimal.Decimal, globalMCCRules map[string]string) (account string, newDescription string, ok bool) {
	for _, r := range e.DescriptionRules {
		if !r.re.MatchString(description) {
			continue
		}
		if r.amount != nil && !amount.Equal(*r.amount) {
			continue
		}
		return r.Account, r.NewDescription, true
	}
	if account, ok := e.MCCRules[category]; ok {
		return account, "", ok
	}
	account, ok = globalMCCRules[category]
	return account, "", ok
}

type currencyRateEntry struct {
	Rate      string    `yaml:"rate"`
	UpdatedAt time.Time `yaml:"updated_at,omitempty"`
}

type Config struct {
	Path             string                       `yaml:"-"`
	Book             string                       `yaml:"book"`
	Sources          Sources                      `yaml:"sources"`
	MCCRules         map[string]string            `yaml:"mcc_rules,omitempty"`
	Accounts         []AccountEntry               `yaml:"accounts"`
	CurrencyCacheTTL string                       `yaml:"currency_cache_ttl,omitempty"`
	CurrencyCache    map[string]currencyRateEntry `yaml:"currency_cache,omitempty"`
	// RateHistory holds official rates as they stood on a given day, keyed
	// "YYYY-MM-DD" then "FROM/TO". Unlike CurrencyCache these never expire: the
	// rate for a past date does not change.
	RateHistory      map[string]map[string]string `yaml:"rate_history,omitempty"`
	currencyCacheTTL time.Duration
}

func (c *Config) currencyCacheTTLDuration() time.Duration {
	if c.currencyCacheTTL > 0 {
		return c.currencyCacheTTL
	}
	return DefaultCurrencyCacheTTL
}

func (c *Config) rateEntryValid(entry currencyRateEntry, now time.Time) bool {
	if entry.UpdatedAt.IsZero() {
		return false
	}
	return now.Sub(entry.UpdatedAt) < c.currencyCacheTTLDuration()
}

// GetRate returns the cached exchange rate for the given currency pair when it
// has not expired. The key is "FROM/TO", e.g. GetRate("USD","UAH") → rate meaning 1 USD = rate UAH.
func (c *Config) GetRate(from, to string) (decimal.Decimal, bool) {
	return c.GetRateAt(from, to, time.Now())
}

// GetRateAt is like GetRate but uses the given time for TTL checks (mainly for tests).
func (c *Config) GetRateAt(from, to string, now time.Time) (decimal.Decimal, bool) {
	entry, ok := c.CurrencyCache[from+"/"+to]
	if !ok || !c.rateEntryValid(entry, now) {
		return decimal.Zero, false
	}
	rate, err := decimal.NewFromString(entry.Rate)
	if err != nil {
		return decimal.Zero, false
	}
	return rate, true
}

// ratePivot is the currency every rate source used here quotes against, and so
// the one cross rates are derived through.
const ratePivot = "UAH"

// rateDateKey formats a date as it is keyed in RateHistory.
func rateDateKey(date time.Time) string { return date.Format("2006-01-02") }

// convertWith converts amount between two currencies using lookup, which reports
// the rate meaning "1 from = rate to". Sources publish only one direction of each
// pair — Monobank quotes EUR/USD but never USD/EUR, and NBU quotes nothing but
// X/UAH — so the inverse pair is used by division, and a missing cross rate is
// derived through ratePivot.
func convertWith(amount decimal.Decimal, from, to string, lookup func(a, b string) (decimal.Decimal, bool)) (decimal.Decimal, bool) {
	if from == to {
		return amount, true
	}
	if rate, ok := lookup(from, to); ok {
		return amount.Mul(rate), true
	}
	if rate, ok := lookup(to, from); ok {
		return amount.Div(rate), true
	}
	if from != ratePivot && to != ratePivot {
		fromPivot, fromOK := lookup(from, ratePivot)
		toPivot, toOK := lookup(to, ratePivot)
		if fromOK && toOK {
			return amount.Mul(fromPivot).Div(toPivot), true
		}
	}
	return decimal.Zero, false
}

// ConvertAmount converts amount between currencies at the current cached rates,
// returning false when the pair cannot be resolved.
func (c *Config) ConvertAmount(amount decimal.Decimal, from, to string) (decimal.Decimal, bool) {
	return convertWith(amount, from, to, func(a, b string) (decimal.Decimal, bool) {
		rate, ok := c.GetRate(a, b)
		return rate, ok && !rate.IsZero()
	})
}

// ConvertAmountOn converts amount between currencies at the rates recorded for
// the given date, returning false when that date has no usable rates. It never
// falls back to current rates: the caller decides whether that substitution is
// acceptable.
func (c *Config) ConvertAmountOn(amount decimal.Decimal, from, to string, date time.Time) (decimal.Decimal, bool) {
	return convertWith(amount, from, to, func(a, b string) (decimal.Decimal, bool) {
		return c.historicalRate(date, a, b)
	})
}

func (c *Config) historicalRate(date time.Time, from, to string) (decimal.Decimal, bool) {
	raw, ok := c.RateHistory[rateDateKey(date)][from+"/"+to]
	if !ok {
		return decimal.Zero, false
	}
	rate, err := decimal.NewFromString(raw)
	if err != nil || rate.IsZero() {
		return decimal.Zero, false
	}
	return rate, true
}

// HasHistoricalRates reports whether rates for the given date have already been
// recorded, so callers can avoid re-fetching a date they have already resolved.
func (c *Config) HasHistoricalRates(date time.Time) bool {
	return len(c.RateHistory[rateDateKey(date)]) > 0
}

// SetHistoricalRates records the rates in effect on the given date. Call
// SaveRates to persist. Rates for a past date are immutable, so an existing
// entry for the date is kept and only missing pairs are added.
func (c *Config) SetHistoricalRates(date time.Time, rates map[string]decimal.Decimal) {
	if len(rates) == 0 {
		return
	}
	if c.RateHistory == nil {
		c.RateHistory = make(map[string]map[string]string)
	}
	key := rateDateKey(date)
	if c.RateHistory[key] == nil {
		c.RateHistory[key] = make(map[string]string, len(rates))
	}
	for pair, rate := range rates {
		if _, exists := c.RateHistory[key][pair]; !exists {
			c.RateHistory[key][pair] = rate.String()
		}
	}
}

// SetRate stores an exchange rate in the in-memory cache. Call Save to persist.
func (c *Config) SetRate(from, to string, rate decimal.Decimal) {
	if c.CurrencyCache == nil {
		c.CurrencyCache = make(map[string]currencyRateEntry)
	}
	c.CurrencyCache[from+"/"+to] = currencyRateEntry{
		Rate:      rate.String(),
		UpdatedAt: time.Now(),
	}
}

// Save writes the entire config to the file it was loaded from.
// Prefer SaveCurrencyCache when only exchange rates changed.
func (c *Config) Save() error {
	if c.Path == "" {
		return nil
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.Path, data, 0600)
}

// SaveCurrencyCache persists only currency_cache by patching that section in the
// config file, leaving user formatting elsewhere intact.
func (c *Config) SaveCurrencyCache() error {
	return c.SaveRates()
}

// SaveRates persists the currency_cache and rate_history sections by patching
// just those parts of the config file, leaving user formatting elsewhere intact.
func (c *Config) SaveRates() error {
	if c.Path == "" {
		return nil
	}
	return patchRateSectionsInFile(c.Path, c.CurrencyCache, c.RateHistory)
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.Path = path
	ttl, err := parseCurrencyCacheTTL(cfg.CurrencyCacheTTL)
	if err != nil {
		return nil, err
	}
	cfg.currencyCacheTTL = ttl
	aliases := make(map[string]string)
	for i := range cfg.Accounts {
		e := &cfg.Accounts[i]
		if e.Alias != "" {
			if other, ok := aliases[e.Alias]; ok {
				return nil, fmt.Errorf("accounts: duplicate alias %q (%s and %s)", e.Alias, other, e.SourceID)
			}
			aliases[e.Alias] = e.SourceID
		}
		for j := range e.DescriptionRules {
			r := &e.DescriptionRules[j]
			re, err := regexp.Compile(r.Pattern)
			if err != nil {
				return nil, fmt.Errorf("description_rules: invalid pattern %q: %w", r.Pattern, err)
			}
			r.re = re
			if r.Amount != "" {
				amt, err := decimal.NewFromString(r.Amount)
				if err != nil {
					return nil, fmt.Errorf("description_rules: invalid amount %q: %w", r.Amount, err)
				}
				r.amount = &amt
			}
		}
	}
	return &cfg, nil
}

func parseCurrencyCacheTTL(s string) (time.Duration, error) {
	if s == "" {
		return DefaultCurrencyCacheTTL, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("currency_cache_ttl: invalid duration %q: %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("currency_cache_ttl: must be positive")
	}
	return d, nil
}

func (c *Config) AccountMapping(sourceID string) (AccountEntry, bool) {
	for _, e := range c.Accounts {
		if e.SourceID == sourceID {
			return e, true
		}
	}
	return AccountEntry{}, false
}

// ResolveAccountRef returns the source_id for ref, which may be a source_id or alias.
func (c *Config) ResolveAccountRef(ref string) (string, error) {
	for _, e := range c.Accounts {
		if e.SourceID == ref {
			return e.SourceID, nil
		}
	}
	for _, e := range c.Accounts {
		if e.Alias != "" && e.Alias == ref {
			return e.SourceID, nil
		}
	}
	return "", fmt.Errorf("no account with source_id or alias %q", ref)
}
