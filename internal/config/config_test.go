package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/config"
)

func loadConfig(t *testing.T, yml string) *config.Config {
	t.Helper()
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	f.WriteString(yml)
	f.Close()
	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

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

func TestConfig_GetRate_Expired(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetRate("USD", "UAH", decimal.NewFromFloat(41.5))

	future := time.Now().Add(config.DefaultCurrencyCacheTTL + time.Minute)
	_, ok := cfg.GetRateAt("USD", "UAH", future)
	if ok {
		t.Fatal("expected expired rate to be treated as missing")
	}
}

func TestConfig_GetRate_LegacyEntryWithoutUpdatedAt(t *testing.T) {
	cfg := loadConfig(t, `
currency_cache:
  USD/UAH:
    rate: "41.5"
`)
	_, ok := cfg.GetRate("USD", "UAH")
	if ok {
		t.Fatal("expected legacy cache entry without updated_at to be treated as expired")
	}
}

func TestLoad_CurrencyCacheTTL(t *testing.T) {
	cfg := loadConfig(t, `
currency_cache_ttl: 1h
`)
	cfg.SetRate("USD", "UAH", decimal.NewFromFloat(41.5))

	base := time.Now()
	_, ok := cfg.GetRateAt("USD", "UAH", base.Add(30*time.Minute))
	if !ok {
		t.Fatal("expected rate to be valid within custom TTL")
	}
	_, ok = cfg.GetRateAt("USD", "UAH", base.Add(2*time.Hour))
	if ok {
		t.Fatal("expected rate to expire after custom TTL")
	}
}

func TestLoad_InvalidCurrencyCacheTTL(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	f.WriteString("currency_cache_ttl: not-a-duration\n")
	f.Close()

	_, err := config.Load(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid currency_cache_ttl")
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
	if err := cfg.SaveCurrencyCache(); err != nil {
		t.Fatalf("SaveCurrencyCache: %v", err)
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

func TestConfig_SaveCurrencyCache_PreservesAccountsWhenFiltered(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	_, err := f.WriteString(`book: /tmp/test.gnucash
accounts:
  - source_id: "UA111"
    gnucash_account: "Assets:One"
  - source_id: "UA222"
    gnucash_account: "Assets:Two"
`)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the old --account bug: in-memory config keeps only one account.
	cfg.Accounts = []config.AccountEntry{cfg.Accounts[0]}
	cfg.SetRate("USD", "UAH", decimal.NewFromFloat(41.5))
	if err := cfg.SaveCurrencyCache(); err != nil {
		t.Fatalf("SaveCurrencyCache: %v", err)
	}

	cfg2, err := config.Load(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg2.Accounts) != 2 {
		t.Fatalf("expected 2 accounts on disk, got %d", len(cfg2.Accounts))
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

	got, _, ok := entry.ResolveCounterpart("SILPO supermarket", "5411", decimal.Zero, nil)
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

	got, _, ok := entry.ResolveCounterpart("UBER ride", "5411", decimal.Zero, nil)
	if !ok {
		t.Fatal("expected match via MCC fallback")
	}
	if got != "Imbalance-UAH" {
		t.Errorf("got %q, want Imbalance-UAH", got)
	}
}

func TestResolveCounterpart_GlobalMCCFallback(t *testing.T) {
	cfg := loadConfig(t, `
mcc_rules:
  "5411": "Expenses:Food:Global"
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
`)
	entry, ok := cfg.AccountMapping("UA123")
	if !ok {
		t.Fatal("no mapping for UA123")
	}

	got, _, ok := entry.ResolveCounterpart("some store", "5411", decimal.Zero, cfg.MCCRules)
	if !ok {
		t.Fatal("expected match via global MCC fallback")
	}
	if got != "Expenses:Food:Global" {
		t.Errorf("got %q, want Expenses:Food:Global", got)
	}
}

func TestResolveCounterpart_PerAccountMCCWinsOverGlobal(t *testing.T) {
	cfg := loadConfig(t, `
mcc_rules:
  "5411": "Expenses:Food:Global"
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    mcc_rules:
      "5411": "Expenses:Food:Local"
`)
	entry, ok := cfg.AccountMapping("UA123")
	if !ok {
		t.Fatal("no mapping for UA123")
	}

	got, _, ok := entry.ResolveCounterpart("some store", "5411", decimal.Zero, cfg.MCCRules)
	if !ok {
		t.Fatal("expected match")
	}
	if got != "Expenses:Food:Local" {
		t.Errorf("got %q, want Expenses:Food:Local", got)
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

	got, _, ok := entry.ResolveCounterpart("SILPO store", "", decimal.Zero, nil)
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

	_, _, ok := entry.ResolveCounterpart("UNKNOWN store", "9999", decimal.Zero, nil)
	if ok {
		t.Fatal("expected no match")
	}
}

func TestResolveCounterpart_DescriptionRuleWithAmount(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "Subscription"
        amount: "-99.00"
        account: "Expenses:Subscriptions"
      - pattern: "Subscription"
        account: "Expenses:Other"
`, "UA123")

	got, _, ok := entry.ResolveCounterpart("Monthly Subscription", "", decimal.RequireFromString("-99.00"), nil)
	if !ok {
		t.Fatal("expected match")
	}
	if got != "Expenses:Subscriptions" {
		t.Errorf("got %q, want Expenses:Subscriptions", got)
	}

	got, _, ok = entry.ResolveCounterpart("Monthly Subscription", "", decimal.RequireFromString("-50.00"), nil)
	if !ok {
		t.Fatal("expected fallback match when amount differs")
	}
	if got != "Expenses:Other" {
		t.Errorf("got %q, want Expenses:Other", got)
	}
}

func TestLoad_InvalidDescriptionAmount(t *testing.T) {
	yml := `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "foo"
        amount: "not-a-number"
        account: "Expenses:Food"
`
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	f.WriteString(yml)
	f.Close()

	_, err := config.Load(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid amount")
	}
}

func TestResolveCounterpart_DescriptionRuleNewDescription(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "PAYPAL"
        new_description: "PayPal payment"
        account: "Expenses:Online"
`, "UA123")

	got, newDesc, ok := entry.ResolveCounterpart("PAYPAL *SHOP 123", "", decimal.Zero, nil)
	if !ok {
		t.Fatal("expected match")
	}
	if got != "Expenses:Online" {
		t.Errorf("got account %q, want Expenses:Online", got)
	}
	if newDesc != "PayPal payment" {
		t.Errorf("got new_description %q, want PayPal payment", newDesc)
	}
}

func TestResolveCounterpart_SkipRule(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "Cashback"
        account: SKIP
`, "UA123")

	got, _, ok := entry.ResolveCounterpart("Cashback reward", "", decimal.Zero, nil)
	if !ok {
		t.Fatal("expected match")
	}
	if got != config.SkipAccount {
		t.Errorf("got %q, want %q", got, config.SkipAccount)
	}
}
