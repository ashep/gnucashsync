package gnucash_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/gnucash"
)

func TestWrite_RoundTrip(t *testing.T) {
	path := writeSampleGnuCash(t)
	book, err := gnucash.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	xml := gnucash.NewTransactionXML(
		"new-txn-001", "Test import", "UAH",
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		decimal.NewFromFloat(-200.00),
		"a0000000000000000000000000000003",
		"a0000000000000000000000000000004",
	)

	if err := gnucash.Write(book, []string{xml}, path); err != nil {
		t.Fatal(err)
	}

	// Re-read and verify the new transaction is present
	book2, err := gnucash.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !book2.SourceIDs["new-txn-001"] {
		t.Error("expected new-txn-001 in re-read book")
	}
	if !book2.SourceIDs["existing-001"] {
		t.Error("original transaction should still be present")
	}
	if book2.TxnCount != 2 {
		t.Errorf("expected TxnCount=2, got %d", book2.TxnCount)
	}
}

func TestWrite_CreatesBackup(t *testing.T) {
	path := writeSampleGnuCash(t)
	book, _ := gnucash.ReadFile(path)

	if err := gnucash.Write(book, nil, path); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Dir(path)
	entries, _ := os.ReadDir(dir)
	var backups []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bak") {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) != 1 {
		t.Errorf("expected 1 backup, got %d: %v", len(backups), backups)
	}
}

func TestWrite_PrunesOldBackups(t *testing.T) {
	path := writeSampleGnuCash(t)

	// Write 12 times to generate 12 backups; only 10 should survive.
	for i := 0; i < 12; i++ {
		book, _ := gnucash.ReadFile(path)
		if err := gnucash.Write(book, nil, path); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	dir := filepath.Dir(path)
	entries, _ := os.ReadDir(dir)
	var backups []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bak") {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) != 10 {
		t.Errorf("expected 10 backups, got %d", len(backups))
	}
}

func TestWrite_RefusesWhenLocked(t *testing.T) {
	path := writeSampleGnuCash(t)
	lckPath := path + ".LCK"
	os.WriteFile(lckPath, []byte{}, 0644)
	defer os.Remove(lckPath)

	book, _ := gnucash.ReadFile(path)
	err := gnucash.Write(book, nil, path)
	if err == nil {
		t.Fatal("expected error when .LCK file exists")
	}
}

func TestWrite_CountDataUpdated(t *testing.T) {
	path := writeSampleGnuCash(t)
	book, _ := gnucash.ReadFile(path)

	xmls := make([]string, 3)
	for i := range xmls {
		xmls[i] = gnucash.NewTransactionXML(
			fmt.Sprintf("new-%d", i), "desc", "UAH",
			time.Now(),
			decimal.NewFromFloat(-10),
			"a0000000000000000000000000000003",
			"a0000000000000000000000000000004",
		)
	}

	gnucash.Write(book, xmls, path)
	book2, _ := gnucash.ReadFile(path)
	if book2.TxnCount != 4 {
		t.Errorf("expected TxnCount=4, got %d", book2.TxnCount)
	}
}
