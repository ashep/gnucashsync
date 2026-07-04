package source_test

import (
	"testing"

	"github.com/ashep/gnucashsync/internal/source"
)

func TestPrivatBankSource_Transactions(t *testing.T) {
	s := source.NewPrivatBank("../../testdata/privatbank.csv")
	txns, err := s.Transactions()
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txns))
	}

	t0 := txns[0]
	if t0.AccountID != "UA987" {
		t.Errorf("AccountID: got %q", t0.AccountID)
	}
	if t0.Description != "SILPO" {
		t.Errorf("Description: got %q", t0.Description)
	}
	if t0.Amount.String() != "-450" {
		t.Errorf("Amount: got %s", t0.Amount.String())
	}
	if t0.Currency != "UAH" {
		t.Errorf("Currency: got %q", t0.Currency)
	}
	if t0.ID == "" {
		t.Error("ID should not be empty")
	}
	// IDs must be unique
	if txns[0].ID == txns[1].ID {
		t.Error("IDs should be unique across transactions")
	}
}
