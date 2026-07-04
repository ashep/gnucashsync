package importer

import (
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/ashep/gnucashsync/internal/config"
	"github.com/ashep/gnucashsync/internal/gnucash"
	"github.com/ashep/gnucashsync/internal/model"
	"github.com/ashep/gnucashsync/internal/source"
)

// Options controls optional behaviour of Run.
type Options struct {
	DryRun bool
	Since  time.Time // zero means no filter
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

	sort.Slice(txns, func(i, j int) bool {
		return txns[i].Date.Before(txns[j].Date)
	})

	var (
		result  Result
		txnXMLs []string
	)

	for _, t := range txns {
		if !opts.Since.IsZero() && t.Date.Before(opts.Since) {
			continue
		}

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

		counterpart, ok := entry.ResolveCounterpart(t.Description, t.Category)
		if !ok {
			category := t.Category
			if t.CategoryLabel != "" {
				category += " (" + t.CategoryLabel + ")"
			}
			return Result{}, fmt.Errorf(
				"no counterpart configured for account %q category %s\n  transaction: %s | %s %s | %s",
				t.AccountID, category, t.Date.Format("2006-01-02"), t.Amount.StringFixed(2), t.Currency, t.Description,
			)
		}

		debitGUID, err := gnucash.ResolveAccount(book, entry.GnuCashAccount)
		if err != nil {
			return Result{}, err
		}

		creditGUID, err := gnucash.ResolveAccount(book, counterpart)
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
