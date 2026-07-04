package config_test

import (
	"os"
	"testing"

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
