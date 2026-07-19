package source_test

import (
	"testing"

	"github.com/ashep/gnucashsync/internal/source"
)

func TestPrivatBankXLSXSource_Transactions(t *testing.T) {
	s := source.NewPrivatBankXLSX("../../testdata/privatbank.xlsx")
	txns, err := s.Transactions()
	if err != nil {
		t.Fatal(err)
	}

	// The testdata file has 9 data rows.
	if len(txns) != 9 {
		t.Fatalf("expected 9 transactions, got %d", len(txns))
	}

	t0 := txns[0]
	if t0.AccountID != "4731 **** **** 2655" {
		t.Errorf("AccountID: got %q", t0.AccountID)
	}
	if t0.Description != "На свою картку *5035" {
		t.Errorf("Description: got %q", t0.Description)
	}
	if t0.Amount.String() != "-110" {
		t.Errorf("Amount: got %s", t0.Amount.String())
	}
	if t0.Currency != "UAH" {
		t.Errorf("Currency: got %q", t0.Currency)
	}
	if t0.Category != "Переказ на свою картку" {
		t.Errorf("Category: got %q", t0.Category)
	}
	if t0.OperationCurrency != "" {
		t.Errorf("OperationCurrency should be empty for same-currency txn, got %q", t0.OperationCurrency)
	}
	if t0.ID == "" {
		t.Error("ID should not be empty")
	}

	// IDs must be unique.
	seen := make(map[string]bool)
	for _, tx := range txns {
		if seen[tx.ID] {
			t.Errorf("duplicate ID %q", tx.ID)
		}
		seen[tx.ID] = true
	}
}
