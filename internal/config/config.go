package config

import (
	"fmt"
	"os"
	"regexp"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"
)

const SkipAccount = "SKIP"

type MonobankSource struct {
	Token string `yaml:"token"`
}

type Sources struct {
	Monobank MonobankSource `yaml:"monobank"`
}

type DescriptionRule struct {
	Pattern string `yaml:"pattern"`
	Account string `yaml:"account"`
	re      *regexp.Regexp
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
func (e *AccountEntry) ResolveCounterpart(description, category string, globalMCCRules map[string]string) (string, bool) {
	for _, r := range e.DescriptionRules {
		if r.re.MatchString(description) {
			return r.Account, true
		}
	}
	if account, ok := e.MCCRules[category]; ok {
		return account, ok
	}
	account, ok := globalMCCRules[category]
	return account, ok
}

type currencyRateEntry struct {
	Rate string `yaml:"rate"`
}

type Config struct {
	Path          string                       `yaml:"-"`
	Book          string                       `yaml:"book"`
	Sources       Sources                      `yaml:"sources"`
	MCCRules      map[string]string            `yaml:"mcc_rules,omitempty"`
	Accounts      []AccountEntry               `yaml:"accounts"`
	CurrencyCache map[string]currencyRateEntry `yaml:"currency_cache,omitempty"`
}

// GetRate returns the cached exchange rate for the given currency pair.
// The key is "FROM/TO", e.g. GetRate("USD","UAH") → rate meaning 1 USD = rate UAH.
func (c *Config) GetRate(from, to string) (decimal.Decimal, bool) {
	entry, ok := c.CurrencyCache[from+"/"+to]
	if !ok {
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
	c.CurrencyCache[from+"/"+to] = currencyRateEntry{Rate: rate.String()}
}

// Save writes the config (including any cached rates) back to the file it was loaded from.
// Does nothing if Path is empty.
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
	for i := range cfg.Accounts {
		for j := range cfg.Accounts[i].DescriptionRules {
			r := &cfg.Accounts[i].DescriptionRules[j]
			re, err := regexp.Compile(r.Pattern)
			if err != nil {
				return nil, fmt.Errorf("description_rules: invalid pattern %q: %w", r.Pattern, err)
			}
			r.re = re
		}
	}
	return &cfg, nil
}

func (c *Config) AccountMapping(sourceID string) (AccountEntry, bool) {
	for _, e := range c.Accounts {
		if e.SourceID == sourceID {
			return e, true
		}
	}
	return AccountEntry{}, false
}
