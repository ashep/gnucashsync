package importer_test

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/config"
	"github.com/ashep/gnucashsync/internal/importer"
	"github.com/ashep/gnucashsync/internal/model"
	"github.com/ashep/gnucashsync/internal/source"
)

const sampleXML = `<?xml version="1.0" encoding="utf-8" ?>
<gnc-v2 xmlns:gnc="http://www.gnucash.org/XML/gnc"
        xmlns:act="http://www.gnucash.org/XML/act"
        xmlns:book="http://www.gnucash.org/XML/book"
        xmlns:cd="http://www.gnucash.org/XML/cd"
        xmlns:cmdty="http://www.gnucash.org/XML/cmdty"
        xmlns:slot="http://www.gnucash.org/XML/slot"
        xmlns:split="http://www.gnucash.org/XML/split"
        xmlns:trn="http://www.gnucash.org/XML/trn"
        xmlns:ts="http://www.gnucash.org/XML/ts">
<gnc:count-data cd:type="book">1</gnc:count-data>
<gnc:book version="2.0.0">
<book:id type="guid">a0000000000000000000000000000000</book:id>
<gnc:count-data cd:type="account">5</gnc:count-data>
<gnc:count-data cd:type="transaction">0</gnc:count-data>
<gnc:account version="2.0.0">
  <act:name>Root Account</act:name>
  <act:id type="guid">a0000000000000000000000000000001</act:id>
  <act:type>ROOT</act:type>
</gnc:account>
<gnc:account version="2.0.0">
  <act:name>Assets</act:name>
  <act:id type="guid">a0000000000000000000000000000002</act:id>
  <act:type>ASSET</act:type>
  <act:commodity><cmdty:space>ISO4217</cmdty:space><cmdty:id>UAH</cmdty:id></act:commodity>
  <act:commodity-scu>100</act:commodity-scu>
  <act:parent type="guid">a0000000000000000000000000000001</act:parent>
</gnc:account>
<gnc:account version="2.0.0">
  <act:name>Monobank UAH</act:name>
  <act:id type="guid">a0000000000000000000000000000003</act:id>
  <act:type>ASSET</act:type>
  <act:commodity><cmdty:space>ISO4217</cmdty:space><cmdty:id>UAH</cmdty:id></act:commodity>
  <act:commodity-scu>100</act:commodity-scu>
  <act:parent type="guid">a0000000000000000000000000000002</act:parent>
</gnc:account>
<gnc:account version="2.0.0">
  <act:name>Imbalance-UAH</act:name>
  <act:id type="guid">a0000000000000000000000000000004</act:id>
  <act:type>INCOME</act:type>
  <act:commodity><cmdty:space>ISO4217</cmdty:space><cmdty:id>UAH</cmdty:id></act:commodity>
  <act:commodity-scu>100</act:commodity-scu>
  <act:parent type="guid">a0000000000000000000000000000001</act:parent>
</gnc:account>
<gnc:account version="2.0.0">
  <act:name>Savings USD</act:name>
  <act:id type="guid">a0000000000000000000000000000005</act:id>
  <act:type>ASSET</act:type>
  <act:commodity><cmdty:space>ISO4217</cmdty:space><cmdty:id>USD</cmdty:id></act:commodity>
  <act:commodity-scu>100</act:commodity-scu>
  <act:parent type="guid">a0000000000000000000000000000002</act:parent>
</gnc:account>
</gnc:book>
</gnc-v2>`

func writeSampleBook(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/book.gnucash"
	f, _ := os.Create(path)
	gz := gzip.NewWriter(f)
	gz.Write([]byte(sampleXML))
	gz.Close()
	f.Close()
	return path
}

func sampleConfig() *config.Config {
	return &config.Config{
		Accounts: []config.AccountEntry{
			{
				SourceID:       "UA123",
				GnuCashAccount: "Assets:Monobank UAH",
				MCCRules:       map[string]string{"5411": "Imbalance-UAH"},
			},
		},
	}
}

func sampleTransactions() []model.Transaction {
	return []model.Transaction{
		{
			ID:          "txn-001",
			Date:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			Description: "Grocery store",
			Amount:      decimal.NewFromFloat(-450),
			Currency:    "UAH",
			AccountID:   "UA123",
			Category:    "5411",
		},
		{
			ID:          "txn-002",
			Date:        time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
			Description: "Salary",
			Amount:      decimal.NewFromFloat(50000),
			Currency:    "UAH",
			AccountID:   "UA123",
			Category:    "5411",
		},
	}
}

func crossCurrencyTxns() []model.Transaction {
	return []model.Transaction{
		{
			ID:          "txn-usd",
			Date:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			Description: "Transfer",
			Amount:      decimal.NewFromFloat(-4150),
			Currency:    "UAH",
			AccountID:   "UA123",
			Category:    "6011",
		},
	}
}

func crossCurrencyConfig(rateFetched bool) *config.Config {
	cfg := &config.Config{
		Accounts: []config.AccountEntry{
			{
				SourceID:       "UA123",
				GnuCashAccount: "Assets:Monobank UAH",
				MCCRules:       map[string]string{"6011": "Assets:Savings USD"},
			},
		},
	}
	if rateFetched {
		// USD/UAH = 41.5 → -4150 UAH / 41.5 = -100 USD → credit qty = +10000/100
		cfg.SetRate("USD", "UAH", decimal.NewFromFloat(41.5))
	}
	return cfg
}

func crossCurrencyConfigExpiredRate(t *testing.T) *config.Config {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-config.DefaultCurrencyCacheTTL - time.Hour).UTC().Format(time.RFC3339)
	_, err = fmt.Fprintf(f, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Monobank UAH"
    mcc_rules:
      "6011": "Assets:Savings USD"
currency_cache:
  USD/UAH:
    rate: "40.0"
    updated_at: %q
`, old)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// readBookRaw returns the decompressed bytes of the GnuCash file.
func readBookRaw(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(gz); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRun_ImportsNewTransactions(t *testing.T) {
	path := writeSampleBook(t)
	result, err := importer.Run(source.NewSlice(sampleTransactions()), path, sampleConfig(), importer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 2 {
		t.Errorf("expected Imported=2, got %d", result.Imported)
	}
	if result.SkippedDuplicate != 0 {
		t.Errorf("expected SkippedDuplicate=0, got %d", result.SkippedDuplicate)
	}
	if len(result.Transactions) != 2 {
		t.Errorf("expected 2 Transactions in result, got %d", len(result.Transactions))
	}
}

func TestRun_SkipsDuplicates(t *testing.T) {
	path := writeSampleBook(t)
	src := source.NewSlice(sampleTransactions())

	// First run.
	importer.Run(src, path, sampleConfig(), importer.Options{})

	// Second run — same source, should be all duplicates.
	result, err := importer.Run(src, path, sampleConfig(), importer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 0 {
		t.Errorf("expected Imported=0 on second run, got %d", result.Imported)
	}
	if result.SkippedDuplicate != 2 {
		t.Errorf("expected SkippedDuplicate=2, got %d", result.SkippedDuplicate)
	}
}

func TestRun_SkipsUnmapped(t *testing.T) {
	path := writeSampleBook(t)
	result, err := importer.Run(source.NewSlice(sampleTransactions()), path, &config.Config{}, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedUnmapped != 2 {
		t.Errorf("expected SkippedUnmapped=2, got %d", result.SkippedUnmapped)
	}
}

func TestRun_DryRunDoesNotWrite(t *testing.T) {
	path := writeSampleBook(t)
	info, _ := os.Stat(path)
	before := info.ModTime()

	result, err := importer.Run(source.NewSlice(sampleTransactions()), path, sampleConfig(), importer.Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}

	info, _ = os.Stat(path)
	if info.ModTime() != before {
		t.Error("dry-run modified the file")
	}
	if result.Imported != 2 {
		t.Errorf("expected Imported=2, got %d", result.Imported)
	}
	if len(result.Transactions) != 2 {
		t.Errorf("expected 2 transactions in result, got %d", len(result.Transactions))
	}
	if result.Transactions[0].ID != "txn-001" {
		t.Errorf("expected first transaction ID txn-001 (oldest first), got %s", result.Transactions[0].ID)
	}
	if result.Transactions[1].ID != "txn-002" {
		t.Errorf("expected second transaction ID txn-002 (oldest first), got %s", result.Transactions[1].ID)
	}
}

func TestRun_DescriptionRuleOverridesMCC(t *testing.T) {
	path := writeSampleBook(t)

	txns := []model.Transaction{
		{
			ID:          "txn-desc",
			Date:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			Description: "Grocery store",
			Amount:      decimal.NewFromFloat(-450),
			Currency:    "UAH",
			AccountID:   "UA123",
			Category:    "5411",
		},
	}

	cfgFile, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	cfgFile.WriteString(`
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Monobank UAH"
    description_rules:
      - pattern: "Grocery"
        account: "Imbalance-UAH"
`)
	cfgFile.Close()

	cfg, err := config.Load(cfgFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	result, err := importer.Run(source.NewSlice(txns), path, cfg, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 {
		t.Errorf("expected Imported=1, got %d", result.Imported)
	}
}

func TestRun_DescriptionRuleNewDescription(t *testing.T) {
	path := writeSampleBook(t)

	txns := []model.Transaction{
		{
			ID:          "txn-rename",
			Date:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			Description: "PAYPAL *SHOP 123",
			Amount:      decimal.NewFromFloat(-100),
			Currency:    "UAH",
			AccountID:   "UA123",
			Category:    "5411",
		},
	}

	cfgFile, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	cfgFile.WriteString(`
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Monobank UAH"
    description_rules:
      - pattern: "PAYPAL"
        new_description: "PayPal payment"
        account: "Imbalance-UAH"
`)
	cfgFile.Close()

	cfg, err := config.Load(cfgFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	result, err := importer.Run(source.NewSlice(txns), path, cfg, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 {
		t.Fatalf("expected Imported=1, got %d", result.Imported)
	}
	if len(result.Transactions) != 1 {
		t.Fatalf("expected 1 transaction in result, got %d", len(result.Transactions))
	}
	if result.Transactions[0].Description != "PayPal payment" {
		t.Errorf("expected rewritten description, got %q", result.Transactions[0].Description)
	}

	raw := string(readBookRaw(t, path))
	if !strings.Contains(raw, "<trn:description>PayPal payment</trn:description>") {
		t.Errorf("expected rewritten description in GnuCash file")
	}
}

func TestRun_SkipsTransactionMatchingSkipRule(t *testing.T) {
	path := writeSampleBook(t)

	txns := []model.Transaction{
		{
			ID:          "txn-skip",
			Date:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			Description: "Cashback reward",
			Amount:      decimal.NewFromFloat(50),
			Currency:    "UAH",
			AccountID:   "UA123",
			Category:    "9999",
		},
	}

	cfgFile, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	cfgFile.WriteString(`
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Monobank UAH"
    description_rules:
      - pattern: "Cashback"
        account: SKIP
`)
	cfgFile.Close()

	cfg, err := config.Load(cfgFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	result, err := importer.Run(source.NewSlice(txns), path, cfg, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 0 {
		t.Errorf("expected Imported=0, got %d", result.Imported)
	}
	if result.SkippedRule != 1 {
		t.Errorf("expected SkippedRule=1, got %d", result.SkippedRule)
	}
}

func TestRun_SinceFilter(t *testing.T) {
	path := writeSampleBook(t)
	since := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	result, err := importer.Run(source.NewSlice(sampleTransactions()), path, sampleConfig(), importer.Options{Since: since})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 {
		t.Errorf("expected Imported=1, got %d", result.Imported)
	}
	if result.Transactions[0].ID != "txn-002" {
		t.Errorf("expected txn-002 (on or after since date), got %s", result.Transactions[0].ID)
	}
}

// TestRun_CrossCurrency_UsesRateFromCache verifies that when a UAH transaction
// maps to a USD counterpart account and the rate is already cached in config,
// the credit split quantity is written in USD, not UAH.
func TestRun_CrossCurrency_UsesRateFromCache(t *testing.T) {
	path := writeSampleBook(t)
	_, err := importer.Run(source.NewSlice(crossCurrencyTxns()), path, crossCurrencyConfig(true), importer.Options{})
	if err != nil {
		t.Fatal(err)
	}

	raw := readBookRaw(t, path)
	// Credit split (USD account) quantity must be +10000/100 (= +100 USD).
	if !strings.Contains(string(raw), "10000/100") {
		t.Error("expected USD credit quantity 10000/100 in book XML")
	}
}

// TestRun_CrossCurrency_FetchesRateWhenMissing verifies that when the rate is
// absent from config, the importer calls RateFetcher, caches the result, and
// uses it to compute the credit split quantity.
func TestRun_CrossCurrency_FetchesRateWhenMissing(t *testing.T) {
	path := writeSampleBook(t)
	cfg := crossCurrencyConfig(false)

	fetchCalled := false
	fakeFetcher := func() (map[string]decimal.Decimal, error) {
		fetchCalled = true
		return map[string]decimal.Decimal{
			"USD/UAH": decimal.NewFromFloat(41.5),
		}, nil
	}

	_, err := importer.Run(source.NewSlice(crossCurrencyTxns()), path, cfg, importer.Options{RateFetcher: fakeFetcher})
	if err != nil {
		t.Fatal(err)
	}

	if !fetchCalled {
		t.Error("expected RateFetcher to be called when rate not in cache")
	}
	rate, ok := cfg.GetRate("USD", "UAH")
	if !ok {
		t.Fatal("expected USD/UAH to be cached after fetch")
	}
	if !rate.Equal(decimal.NewFromFloat(41.5)) {
		t.Errorf("cached rate: expected 41.5, got %s", rate)
	}

	raw := readBookRaw(t, path)
	if !strings.Contains(string(raw), "10000/100") {
		t.Error("expected USD credit quantity 10000/100 in book XML")
	}
}

// TestRun_CrossCurrency_RefetchesExpiredRate verifies that an expired cache entry
// triggers a fresh fetch instead of reusing the stale rate.
func TestRun_CrossCurrency_RefetchesExpiredRate(t *testing.T) {
	path := writeSampleBook(t)
	cfg := crossCurrencyConfigExpiredRate(t)

	fetchCalled := false
	fakeFetcher := func() (map[string]decimal.Decimal, error) {
		fetchCalled = true
		return map[string]decimal.Decimal{
			"USD/UAH": decimal.NewFromFloat(41.5),
		}, nil
	}

	_, err := importer.Run(source.NewSlice(crossCurrencyTxns()), path, cfg, importer.Options{RateFetcher: fakeFetcher})
	if err != nil {
		t.Fatal(err)
	}

	if !fetchCalled {
		t.Fatal("expected RateFetcher to be called when cached rate is expired")
	}
	rate, ok := cfg.GetRate("USD", "UAH")
	if !ok {
		t.Fatal("expected USD/UAH to be cached after fetch")
	}
	if !rate.Equal(decimal.NewFromFloat(41.5)) {
		t.Errorf("cached rate: expected 41.5, got %s", rate)
	}
}
