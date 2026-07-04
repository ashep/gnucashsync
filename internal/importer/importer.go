package importer

import (
	"fmt"
	"log"

	"github.com/ashep/gnucashsync/internal/config"
	"github.com/ashep/gnucashsync/internal/gnucash"
	"github.com/ashep/gnucashsync/internal/model"
	"github.com/ashep/gnucashsync/internal/source"
)

// Options controls optional behaviour of Run.
type Options struct {
	DryRun bool
}

// Result summarizes an import run.
type Result struct {
	Imported         int
	SkippedDuplicate int
	SkippedUnmapped  int
	Transactions     []model.Transaction
}

// Run reads transactions from src, imports new ones into gnucashPath (skipped
// when opts.DryRun is true), and returns a summary.
func Run(src source.Source, gnucashPath string, cfg *config.Config, opts Options) (Result, error) {
	txns, err := src.Transactions()
	if err != nil {
		return Result{}, fmt.Errorf("reading source: %w", err)
	}

	book, err := gnucash.ReadFile(gnucashPath)
	if err != nil {
		return Result{}, fmt.Errorf("reading GnuCash file: %w", err)
	}

	var (
		result  Result
		txnXMLs []string
	)

	for _, t := range txns {
		if book.SourceIDs[t.ID] {
			result.SkippedDuplicate++
			continue
		}

		entry, ok := cfg.AccountMapping(t.AccountID)
		if !ok {
			log.Printf("warning: no account mapping for source_id %q — skipping transaction %q", t.AccountID, t.ID)
			result.SkippedUnmapped++
			continue
		}

		debitGUID, err := gnucash.ResolveAccount(book, entry.GnuCashAccount)
		if err != nil {
			return Result{}, err
		}

		creditGUID, err := gnucash.ResolveAccount(book, entry.DefaultCounterpart)
		if err != nil {
			return Result{}, err
		}

		xml := gnucash.NewTransactionXML(
			t.ID, t.Description, t.Currency, t.Date, t.Amount,
			debitGUID, creditGUID,
		)
		txnXMLs = append(txnXMLs, xml)
		result.Transactions = append(result.Transactions, t)
		result.Imported++
	}

	if opts.DryRun {
		return result, nil
	}

	if err := gnucash.Write(book, txnXMLs, gnucashPath); err != nil {
		return Result{}, fmt.Errorf("writing GnuCash file: %w", err)
	}

	return result, nil
}
