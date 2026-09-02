package config_test

import (
	"os"
	"strings"
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

func TestConfig_SaveCurrencyCache_PreservesFormatting(t *testing.T) {
	const input = `book: /tmp/test.gnucash

sources:
  monobank:
    token: "secret"

accounts:
  - source_id: "UA111"
    gnucash_account: "Assets:One"

mcc_rules:
  "5411": "Expenses:Food"

`
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	if _, err := f.WriteString(input); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetRate("USD", "UAH", decimal.NewFromFloat(41.5))
	if err := cfg.SaveCurrencyCache(); err != nil {
		t.Fatalf("SaveCurrencyCache: %v", err)
	}

	got, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	gotStr := string(got)

	if gotStr == input {
		t.Fatal("expected currency_cache section to be added")
	}
	if !strings.Contains(gotStr, "book: /tmp/test.gnucash\n\nsources:") {
		t.Errorf("blank line after book was removed:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "token: \"secret\"\n\naccounts:") {
		t.Errorf("blank line before accounts was removed:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "Assets:One\"\n\nmcc_rules:") {
		t.Errorf("blank line before mcc_rules was removed:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "currency_cache:") {
		t.Errorf("expected currency_cache section in output:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "USD/UAH") {
		t.Errorf("expected USD/UAH rate in output:\n%s", gotStr)
	}

	cfg2, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	rate, ok := cfg2.GetRate("USD", "UAH")
	if !ok || !rate.Equal(decimal.NewFromFloat(41.5)) {
		t.Fatalf("expected persisted rate 41.5, got %s (ok=%v)", rate, ok)
	}
}

func TestConfig_SaveCurrencyCache_PreservesCurrencyCacheComment(t *testing.T) {
	const input = `book: /tmp/test.gnucash

currency_cache: # auto-populated exchange rates

accounts:
  - source_id: "UA111"
    gnucash_account: "Assets:One"
`
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	if _, err := f.WriteString(input); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetRate("USD", "UAH", decimal.NewFromFloat(41.5))
	if err := cfg.SaveCurrencyCache(); err != nil {
		t.Fatalf("SaveCurrencyCache: %v", err)
	}

	got, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	gotStr := string(got)

	if !strings.Contains(gotStr, "currency_cache: # auto-populated exchange rates") {
		t.Errorf("currency_cache comment was not preserved:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "USD/UAH") {
		t.Errorf("expected new USD/UAH rate in output:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "Assets:One\"\n") {
		t.Errorf("accounts section formatting changed:\n%s", gotStr)
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

func TestLoad_DuplicateAlias(t *testing.T) {
	yml := `
accounts:
  - source_id: "UA111"
    alias: "mono_black"
    gnucash_account: "Assets:One"
  - source_id: "UA222"
    alias: "mono_black"
    gnucash_account: "Assets:Two"
`
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	f.WriteString(yml)
	f.Close()
	_, err := config.Load(f.Name())
	if err == nil {
		t.Fatal("expected error for duplicate alias")
	}
	if !strings.Contains(err.Error(), "duplicate alias") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveAccountRef(t *testing.T) {
	yml := `
accounts:
  - source_id: "UA111"
    alias: "mono_black"
    gnucash_account: "Assets:One"
  - source_id: "UA222"
    alias: "privat_savings"
    gnucash_account: "Assets:Two"
`
	cfg := loadConfig(t, yml)

	got, err := cfg.ResolveAccountRef("mono_black")
	if err != nil {
		t.Fatalf("ResolveAccountRef(alias): %v", err)
	}
	if got != "UA111" {
		t.Errorf("got %q, want UA111", got)
	}

	got, err = cfg.ResolveAccountRef("UA222")
	if err != nil {
		t.Fatalf("ResolveAccountRef(source_id): %v", err)
	}
	if got != "UA222" {
		t.Errorf("got %q, want UA222", got)
	}

	_, err = cfg.ResolveAccountRef("unknown")
	if err == nil {
		t.Fatal("expected error for unknown ref")
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

// TestConvertAmount_UsesInversePair verifies that a pair cached in only one
// direction still converts the other way. Monobank quotes EUR/USD but never
// USD/EUR, so without this a EUR→USD conversion would find no rate at all.
func TestConvertAmount_UsesInversePair(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetRate("EUR", "USD", decimal.NewFromFloat(1.161))

	got, ok := cfg.ConvertAmount(decimal.NewFromFloat(-235.34), "EUR", "USD")
	if !ok {
		t.Fatal("expected direct EUR/USD pair to convert")
	}
	if want := decimal.NewFromFloat(-273.23); !got.Round(2).Equal(want) {
		t.Errorf("EUR→USD: got %s, want %s", got.Round(2), want)
	}

	got, ok = cfg.ConvertAmount(decimal.NewFromFloat(-273.23), "USD", "EUR")
	if !ok {
		t.Fatal("expected inverse USD/EUR conversion via the cached EUR/USD pair")
	}
	if want := decimal.NewFromFloat(-235.34); !got.Round(2).Equal(want) {
		t.Errorf("USD→EUR: got %s, want %s", got.Round(2), want)
	}
}

// TestConvertAmount_NoRate verifies an uncached pair reports failure instead of
// returning the amount unconverted.
func TestConvertAmount_NoRate(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetRate("EUR", "USD", decimal.NewFromFloat(1.161))

	if _, ok := cfg.ConvertAmount(decimal.NewFromInt(100), "CHF", "USD"); ok {
		t.Error("expected CHF→USD to fail with no CHF rate cached")
	}
	if got, ok := cfg.ConvertAmount(decimal.NewFromInt(100), "USD", "USD"); !ok || !got.Equal(decimal.NewFromInt(100)) {
		t.Errorf("same-currency conversion should be identity, got %s ok=%v", got, ok)
	}
}

// TestConvertAmount_TriangulatesViaHryvnia verifies that a cross rate is derived
// through UAH. NBU quotes every currency against the hryvnia only, so EUR→USD
// exists solely as EUR/UAH ÷ USD/UAH.
func TestConvertAmount_TriangulatesViaHryvnia(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetRate("EUR", "UAH", decimal.RequireFromString("52.008"))
	cfg.SetRate("USD", "UAH", decimal.RequireFromString("44.5717"))

	got, ok := cfg.ConvertAmount(decimal.RequireFromString("-235.34"), "EUR", "USD")
	if !ok {
		t.Fatal("expected EUR→USD to triangulate through UAH")
	}
	// 235.34 × 52.008 / 44.5717 = 274.60
	if want := decimal.RequireFromString("-274.60"); !got.Round(2).Equal(want) {
		t.Errorf("EUR→USD: got %s, want %s", got.Round(2), want)
	}
}

// TestConvertAmountOn_UsesDateSpecificRates verifies that a conversion for a
// given date uses that date's recorded rates and ignores the current ones.
func TestConvertAmountOn_UsesDateSpecificRates(t *testing.T) {
	day := time.Date(2026, 8, 27, 10, 59, 0, 0, time.UTC)
	cfg := &config.Config{}
	// A wildly different "current" rate that must not be used.
	cfg.SetRate("EUR", "USD", decimal.RequireFromString("2.0"))
	cfg.SetHistoricalRates(day, map[string]decimal.Decimal{
		"EUR/UAH": decimal.RequireFromString("52.008"),
		"USD/UAH": decimal.RequireFromString("44.5717"),
	})

	got, ok := cfg.ConvertAmountOn(decimal.RequireFromString("-235.34"), "EUR", "USD", day)
	if !ok {
		t.Fatal("expected the historical rates for 2026-08-27 to be used")
	}
	if want := decimal.RequireFromString("-274.60"); !got.Round(2).Equal(want) {
		t.Errorf("got %s, want %s", got.Round(2), want)
	}

	// A date with no recorded rates must report failure, not silently fall back.
	other := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	if _, ok := cfg.ConvertAmountOn(decimal.NewFromInt(1), "EUR", "USD", other); ok {
		t.Error("expected no conversion for a date with no recorded rates")
	}
	if cfg.HasHistoricalRates(other) {
		t.Error("HasHistoricalRates should be false for an unrecorded date")
	}
	if !cfg.HasHistoricalRates(day) {
		t.Error("HasHistoricalRates should be true for a recorded date")
	}
}

// TestSaveRates_PersistsRateHistoryAndPreservesFormatting verifies that the new
// rate_history section round-trips through the file without disturbing the rest
// of the user's config.
func TestSaveRates_PersistsRateHistoryAndPreservesFormatting(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	original := `book: /books/my.gnucash

accounts:
  # my card
  - source_id: "UA123"
    gnucash_account: "Assets:Card"
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetRate("USD", "UAH", decimal.RequireFromString("44.44"))
	cfg.SetHistoricalRates(time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC), map[string]decimal.Decimal{
		"USD/UAH": decimal.RequireFromString("44.5717"),
	})
	if err := cfg.SaveRates(); err != nil {
		t.Fatal(err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(saved)
	for _, want := range []string{
		"book: /books/my.gnucash",
		"  # my card",
		`    gnucash_account: "Assets:Card"`,
		"currency_cache:",
		"rate_history:",
		"2026-08-27",
		"44.5717",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("saved config missing %q:\n%s", want, got)
		}
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.HasHistoricalRates(time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)) {
		t.Error("rate_history did not survive a save/load round trip")
	}
	if len(reloaded.Accounts) != 1 {
		t.Errorf("accounts lost on save: %+v", reloaded.Accounts)
	}
}
