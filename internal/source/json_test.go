package source_test

import (
	"testing"
	"time"

	"github.com/ashep/gnucashsync/internal/source"
)

func TestJSONSource_Transactions(t *testing.T) {
	s := source.NewJSON("../../testdata/transactions.json")
	txns, err := s.Transactions()
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txns))
	}

	t0 := txns[0]
	if t0.ID != "txn-001" {
		t.Errorf("ID: got %q", t0.ID)
	}
	if t0.Description != "Grocery store" {
		t.Errorf("Description: got %q", t0.Description)
	}
	if t0.Amount.String() != "-450" {
		t.Errorf("Amount: got %s", t0.Amount.String())
	}
	if t0.Currency != "UAH" {
		t.Errorf("Currency: got %q", t0.Currency)
	}
	if t0.AccountID != "UA123" {
		t.Errorf("AccountID: got %q", t0.AccountID)
	}
	if t0.Date != time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("Date: got %v", t0.Date)
	}
}
