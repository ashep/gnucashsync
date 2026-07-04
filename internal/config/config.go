package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type MonobankSource struct {
	Token string `yaml:"token"`
}

type Sources struct {
	Monobank MonobankSource `yaml:"monobank"`
}

type AccountEntry struct {
	SourceID           string `yaml:"source_id"`
	GnuCashAccount     string `yaml:"gnucash_account"`
	DefaultCounterpart string `yaml:"default_counterpart"`
}

type Config struct {
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
