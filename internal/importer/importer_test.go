package importer_test

import (
	"bytes"
	"compress/gzip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/config"
	"github.com/ashep/gnucashsync/internal/importer"
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

func TestRun_ImportsNewTransactions(t *testing.T) {
	path := writeSampleBook(t)
	src := source.NewJSON("../../testdata/transactions.json")
	cfg := sampleConfig()

	result, err := importer.Run(src, path, cfg, importer.Options{})
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
	src := source.NewJSON("../../testdata/transactions.json")
	cfg := sampleConfig()

	// First run.
	importer.Run(src, path, cfg, importer.Options{})

	// Second run — same source, should be all duplicates.
	result, err := importer.Run(src, path, cfg, importer.Options{})
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
	src := source.NewJSON("../../testdata/transactions.json")
	// Config with no matching account — all should be skipped as unmapped.
	cfg := &config.Config{}

	result, err := importer.Run(src, path, cfg, importer.Options{})
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

	src := source.NewJSON("../../testdata/transactions.json")
	cfg := sampleConfig()

	result, err := importer.Run(src, path, cfg, importer.Options{DryRun: true})
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

	// Single transaction that matches the description rule "Grocery".
	// The config has NO mcc_rules, so if the description rule doesn't fire
	// the importer would return an error — a passing test proves it fired.
	txnFile, _ := os.CreateTemp(t.TempDir(), "txns*.json")
	txnFile.WriteString(`[{"id":"txn-desc","date":"2026-07-01","description":"Grocery store","amount":-450.00,"currency":"UAH","account_id":"UA123","category":"5411"}]`)
	txnFile.Close()

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

	result, err := importer.Run(source.NewJSON(txnFile.Name()), path, cfg, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 {
		t.Errorf("expected Imported=1, got %d", result.Imported)
	}
}

func TestRun_SkipsTransactionMatchingSkipRule(t *testing.T) {
	path := writeSampleBook(t)

	txnFile, _ := os.CreateTemp(t.TempDir(), "txns*.json")
	txnFile.WriteString(`[{"id":"txn-skip","date":"2026-07-01","description":"Cashback reward","amount":50.00,"currency":"UAH","account_id":"UA123","category":"9999"}]`)
	txnFile.Close()

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

	result, err := importer.Run(source.NewJSON(txnFile.Name()), path, cfg, importer.Options{})
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
	src := source.NewJSON("../../testdata/transactions.json")
	cfg := sampleConfig()

	since := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	result, err := importer.Run(src, path, cfg, importer.Options{Since: since})
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

// crossCurrencyTxnJSON returns a JSON source with one UAH transaction (-4150 UAH)
// that maps to the USD account "Assets:Savings USD" via MCC rule.
func crossCurrencyTxnJSON(t *testing.T) string {
	t.Helper()
	f, _ := os.CreateTemp(t.TempDir(), "txns*.json")
	f.WriteString(`[{"id":"txn-usd","date":"2026-07-01","description":"Transfer","amount":-4150.00,"currency":"UAH","account_id":"UA123","category":"6011"}]`)
	f.Close()
	return f.Name()
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

// TestRun_CrossCurrency_UsesRateFromCache verifies that when a UAH transaction
// maps to a USD counterpart account and the rate is already cached in config,
// the credit split quantity is written in USD, not UAH.
func TestRun_CrossCurrency_UsesRateFromCache(t *testing.T) {
	path := writeSampleBook(t)
	cfg := crossCurrencyConfig(true) // rate pre-loaded in cache

	_, err := importer.Run(source.NewJSON(crossCurrencyTxnJSON(t)), path, cfg, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}

	raw := readBookRaw(t, path)
	// Credit split (USD account) quantity must be +10000/100 (= +100 USD).
	// Debit split (UAH account) value/quantity must be -415000/100.
	if !strings.Contains(string(raw), "10000/100") {
		t.Error("expected USD credit quantity 10000/100 in book XML")
	}
}

// TestRun_CrossCurrency_FetchesRateWhenMissing verifies that when the rate is
// absent from config, the importer calls RateFetcher, caches the result, and
// uses it to compute the credit split quantity.
func TestRun_CrossCurrency_FetchesRateWhenMissing(t *testing.T) {
	path := writeSampleBook(t)
	cfg := crossCurrencyConfig(false) // no rate in cache

	fetchCalled := false
	fakeFetcher := func() (map[string]decimal.Decimal, error) {
		fetchCalled = true
		return map[string]decimal.Decimal{
			"USD/UAH": decimal.NewFromFloat(41.5),
		}, nil
	}

	_, err := importer.Run(source.NewJSON(crossCurrencyTxnJSON(t)), path, cfg,
		importer.Options{RateFetcher: fakeFetcher})
	if err != nil {
		t.Fatal(err)
	}

	if !fetchCalled {
		t.Error("expected RateFetcher to be called when rate not in cache")
	}
	// Rate should now be in the in-memory cache for future transactions.
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
