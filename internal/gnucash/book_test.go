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

func TestAccountByGUID_Found(t *testing.T) {
	book := sampleBook(t)
	a, ok := gnucash.AccountByGUID(book, "a0000000000000000000000000000003")
	if !ok {
		t.Fatal("expected account to be found")
	}
	if a.Name != "Monobank UAH" {
		t.Errorf("got name %q", a.Name)
	}
	if a.Currency != "UAH" {
		t.Errorf("got currency %q", a.Currency)
	}
}

func TestAccountByGUID_NotFound(t *testing.T) {
	book := sampleBook(t)
	_, ok := gnucash.AccountByGUID(book, "doesnotexist")
	if ok {
		t.Fatal("expected not found")
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
		decimal.Zero, "",
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

func TestNewTransactionXML_EscapesSpecialChars(t *testing.T) {
	xml := gnucash.NewTransactionXML(
		`A&B<"C>`, "desc", "UAH",
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		decimal.NewFromFloat(-100),
		"debitguid00000000000000000000001",
		"creditguid0000000000000000000001",
		decimal.Zero, "",
	)
	if strings.Contains(xml, `A&B`) {
		t.Error("sourceID ampersand not escaped")
	}
	if !strings.Contains(xml, "&amp;") {
		t.Error("expected &amp; in output")
	}
	if !strings.Contains(xml, "&lt;") {
		t.Error("expected &lt; in output")
	}
	if !strings.Contains(xml, "&quot;") {
		t.Error("expected &quot; in output")
	}
}

// TestNewTransactionXML_MultiCurrency verifies that when a UAH bank account
// makes a foreign-currency purchase, the counterpart split's quantity is
// written in the operation currency (USD), not in UAH.
func TestNewTransactionXML_MultiCurrency(t *testing.T) {
	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	amount := decimal.NewFromFloat(-2600.00)         // UAH debited from bank account
	operationAmount := decimal.NewFromFloat(-100.00) // USD operation amount (same sign)

	xml := gnucash.NewTransactionXML(
		"txn-mc-01", "Amazon", "UAH", date, amount,
		"debitguid00000000000000000000001",
		"creditguid0000000000000000000001",
		operationAmount, "USD",
	)

	// Debit split (UAH account): value = quantity = -260000/100
	if !strings.Contains(xml, "-260000/100") {
		t.Errorf("expected debit UAH value/quantity -260000/100 in XML:\n%s", xml)
	}
	// Credit split: value = +260000/100 (transaction currency UAH), quantity = +10000/100 (USD)
	if !strings.Contains(xml, "260000/100") {
		t.Errorf("expected credit UAH value 260000/100 in XML:\n%s", xml)
	}
	if !strings.Contains(xml, "10000/100") {
		t.Errorf("expected credit USD quantity 10000/100 in XML:\n%s", xml)
	}
}
