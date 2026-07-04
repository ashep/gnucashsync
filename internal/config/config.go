package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

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
// It checks description_rules first (first match wins), then falls back to mcc_rules.
func (e *AccountEntry) ResolveCounterpart(description, category string) (string, bool) {
	for _, r := range e.DescriptionRules {
		if r.re.MatchString(description) {
			return r.Account, true
		}
	}
	account, ok := e.MCCRules[category]
	return account, ok
}

type Config struct {
	Book     string         `yaml:"book"`
	Sources  Sources        `yaml:"sources"`
	Accounts []AccountEntry `yaml:"accounts"`
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
