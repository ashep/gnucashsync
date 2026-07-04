package config_test

import (
	"os"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/config"
)

func loadEntry(t *testing.T, yml, sourceID string) config.AccountEntry {
	t.Helper()
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	f.WriteString(yml)
	f.Close()
	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, ok := cfg.AccountMapping(sourceID)
	if !ok {
		t.Fatalf("no mapping for %q", sourceID)
	}
	return entry
}

func TestConfig_SetGetRate_RoundTrip(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetRate("USD", "UAH", decimal.NewFromFloat(41.5))
	rate, ok := cfg.GetRate("USD", "UAH")
	if !ok {
		t.Fatal("expected rate to be found after SetRate")
	}
	if !rate.Equal(decimal.NewFromFloat(41.5)) {
		t.Errorf("expected 41.5, got %s", rate)
	}
}

func TestConfig_GetRate_Missing(t *testing.T) {
	cfg := &config.Config{}
	_, ok := cfg.GetRate("USD", "UAH")
	if ok {
		t.Fatal("expected no rate on empty config")
	}
}

func TestConfig_Save_PersistsCurrencyCache(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	f.WriteString("book: /tmp/test.gnucash\n")
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetRate("USD", "UAH", decimal.NewFromFloat(41.5))
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cfg2, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	rate, ok := cfg2.GetRate("USD", "UAH")
	if !ok {
		t.Fatal("expected USD/UAH rate after reload")
	}
	if !rate.Equal(decimal.NewFromFloat(41.5)) {
		t.Errorf("expected 41.5 after reload, got %s", rate)
	}
}

func TestLoad(t *testing.T) {
	yml := `
sources:
  monobank:
    token: "test-token"
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank:Monobank UAH"
    mcc_rules:
      "5411": "Imbalance-UAH"
`
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	f.WriteString(yml)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	entry, ok := cfg.AccountMapping("UA123")
	if !ok {
		t.Fatal("expected mapping for UA123")
	}
	if entry.GnuCashAccount != "Assets:Bank:Monobank UAH" {
		t.Errorf("got %q", entry.GnuCashAccount)
	}
	if entry.MCCRules["5411"] != "Imbalance-UAH" {
		t.Errorf("got %q", entry.MCCRules["5411"])
	}
	if cfg.Sources.Monobank.Token != "test-token" {
		t.Errorf("got token %q", cfg.Sources.Monobank.Token)
	}

	_, ok = cfg.AccountMapping("UNKNOWN")
	if ok {
		t.Fatal("expected no mapping for UNKNOWN")
	}
}

func TestLoad_InvalidDescriptionPattern(t *testing.T) {
	yml := `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "["
        account: "Expenses:Food"
`
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	f.WriteString(yml)
	f.Close()

	_, err := config.Load(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid regexp pattern")
	}
}

func TestResolveCounterpart_DescriptionRuleWins(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "SILPO|АТБ"
        account: "Expenses:Food"
    mcc_rules:
      "5411": "Imbalance-UAH"
`, "UA123")

	got, ok := entry.ResolveCounterpart("SILPO supermarket", "5411")
	if !ok {
		t.Fatal("expected match")
	}
	if got != "Expenses:Food" {
		t.Errorf("got %q, want Expenses:Food", got)
	}
}

func TestResolveCounterpart_MCCFallback(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "SILPO"
        account: "Expenses:Food"
    mcc_rules:
      "5411": "Imbalance-UAH"
`, "UA123")

	got, ok := entry.ResolveCounterpart("UBER ride", "5411")
	if !ok {
		t.Fatal("expected match via MCC fallback")
	}
	if got != "Imbalance-UAH" {
		t.Errorf("got %q, want Imbalance-UAH", got)
	}
}

func TestResolveCounterpart_FirstMatchWins(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "SILPO"
        account: "Expenses:Food:Silpo"
      - pattern: "SILPO|АТБ"
        account: "Expenses:Food"
`, "UA123")

	got, ok := entry.ResolveCounterpart("SILPO store", "")
	if !ok {
		t.Fatal("expected match")
	}
	if got != "Expenses:Food:Silpo" {
		t.Errorf("expected first rule to win, got %q", got)
	}
}

func TestResolveCounterpart_NoMatch(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "SILPO"
        account: "Expenses:Food"
    mcc_rules:
      "5411": "Imbalance-UAH"
`, "UA123")

	_, ok := entry.ResolveCounterpart("UNKNOWN store", "9999")
	if ok {
		t.Fatal("expected no match")
	}
}
