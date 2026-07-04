package gnucash_test

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/gnucash"
)

func sampleBook(t *testing.T) *gnucash.ParsedBook {
	t.Helper()
	path := writeSampleGnuCash(t) // defined in reader_test.go
	book, err := gnucash.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return book
}

func TestResolveAccount_Found(t *testing.T) {
	book := sampleBook(t)
	guid, err := gnucash.ResolveAccount(book, "Assets:Monobank UAH")
	if err != nil {
		t.Fatal(err)
	}
	if guid != "a0000000000000000000000000000003" {
		t.Errorf("unexpected GUID %q", guid)
	}
}

func TestResolveAccount_NotFound(t *testing.T) {
	book := sampleBook(t)
	_, err := gnucash.ResolveAccount(book, "Assets:Nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown account path")
	}
}

func TestNewTransactionXML_Contains(t *testing.T) {
	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	amount := decimal.NewFromFloat(-450.00)
	xml := gnucash.NewTransactionXML(
		"txn-test-01",
		"Grocery store",
		"UAH",
		date,
		amount,
		"debitguid00000000000000000000001",
		"creditguid0000000000000000000001",
	)

	checks := []string{
		"gnucashsync:source-id",
		"txn-test-01",
		"Grocery store",
		"UAH",
		"2026-07-01",
		"-45000/100",
		"45000/100",
		"debitguid00000000000000000000001",
		"creditguid0000000000000000000001",
	}
	for _, c := range checks {
		if !strings.Contains(xml, c) {
			t.Errorf("transaction XML missing %q\nXML:\n%s", c, xml)
		}
	}
}
