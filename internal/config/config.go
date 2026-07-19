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

// GetRateOrZero returns the cached rate, or zero if not found or malformed.
func (c *Config) GetRateOrZero(from, to string) decimal.Decimal {
	rate, _ := c.GetRate(from, to)
	return rate
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
	if c.Path == "" {
		return nil
	}
	return patchCurrencyCacheInFile(c.Path, c.CurrencyCache)
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
	for i := range cfg.Accounts {
		for j := range cfg.Accounts[i].DescriptionRules {
			r := &cfg.Accounts[i].DescriptionRules[j]
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
