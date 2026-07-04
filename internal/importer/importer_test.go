package importer_test

import (
	"compress/gzip"
	"os"
	"testing"

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
<gnc:count-data cd:type="account">4</gnc:count-data>
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
				SourceID:           "UA123",
				GnuCashAccount:     "Assets:Monobank UAH",
				DefaultCounterpart: "Imbalance-UAH",
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
		t.Errorf("expected first transaction ID txn-001, got %s", result.Transactions[0].ID)
	}
	if result.Transactions[1].ID != "txn-002" {
		t.Errorf("expected second transaction ID txn-002, got %s", result.Transactions[1].ID)
	}
}
