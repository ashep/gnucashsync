package importer

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/config"
	"github.com/ashep/gnucashsync/internal/gnucash"
	"github.com/ashep/gnucashsync/internal/model"
	"github.com/ashep/gnucashsync/internal/source"
)

// Options controls optional behaviour of Run.
type Options struct {
	DryRun      bool
	Since       time.Time // zero means no filter
	Until       time.Time // zero means no filter
	RateFetcher func() (map[string]decimal.Decimal, error)
}

// Result summarizes an import run.
type Result struct {
	Imported         int
	SkippedDuplicate int
	SkippedUnmapped  int
	SkippedRule      int
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

	accountOrder := make(map[string]int, len(cfg.Accounts))
	for i, a := range cfg.Accounts {
		accountOrder[a.SourceID] = i
	}
	sort.SliceStable(txns, func(i, j int) bool {
		oi, oj := accountOrder[txns[i].AccountID], accountOrder[txns[j].AccountID]
		if oi != oj {
			return oi < oj
		}
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
		if !opts.Until.IsZero() && t.Date.After(opts.Until) {
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

		t.Description = strings.ReplaceAll(t.Description, "\n", "; ")

		counterpart, ok := entry.ResolveCounterpart(t.Description, t.Category)
		if ok && counterpart == config.SkipAccount {
			log.Printf("skipping transaction %q: matched SKIP rule (%s)", t.ID, t.Description)
			result.SkippedRule++
			continue
		}
		if !ok {
			category := t.Category
			if t.CategoryLabel != "" {
				category += " (" + t.CategoryLabel + ")"
			}
			msg := fmt.Sprintf(
				"no counterpart configured for account %q category %s\n  transaction: %s | %s %s | %s",
				t.AccountID, category, t.Date.Format("2006-01-02"), t.Amount.StringFixed(2), t.Currency, t.Description,
			)
			if !opts.DryRun {
				return Result{}, fmt.Errorf("%s", msg)
			}
			log.Printf("warning: %s", msg)
			result.SkippedUnmapped++
			continue
		}

		debitGUID, err := gnucash.ResolveAccount(book, entry.GnuCashAccount)
		if err != nil {
			return Result{}, err
		}

		creditGUID, err := gnucash.ResolveAccount(book, counterpart)
		if err != nil {
			return Result{}, err
		}

		opAmount := t.OperationAmount
		opCurrency := t.OperationCurrency

		// If Monobank did not supply a foreign-currency amount, check whether
		// the counterpart GnuCash account is in a different currency and compute
		// the quantity from the cached (or freshly fetched) exchange rate.
		if opCurrency == "" {
			if counterpart, ok := gnucash.AccountByGUID(book, creditGUID); ok && counterpart.Currency != t.Currency {
				rate, cached := cfg.GetRate(counterpart.Currency, t.Currency)
				if !cached {
					fetcher := opts.RateFetcher
					if fetcher == nil {
						fetcher = source.FetchRates
					}
					rates, err := fetcher()
					if err != nil {
						return Result{}, fmt.Errorf("fetching exchange rates: %w", err)
					}
					for k, v := range rates {
						// parse "FROM/TO" key and store in config cache
						if len(k) == 7 && k[3] == '/' {
							cfg.SetRate(k[:3], k[4:], v)
						}
					}
					if err := cfg.Save(); err != nil {
						log.Printf("warning: could not save rate cache: %v", err)
					}
					rate = cfg.GetRateOrZero(counterpart.Currency, t.Currency)
				}
				if !rate.IsZero() {
					opAmount = t.Amount.Div(rate)
					opCurrency = counterpart.Currency
				}
			}
		}

		xml := gnucash.NewTransactionXML(
			t.ID, t.Description, t.Currency, t.Date, t.Amount,
			debitGUID, creditGUID,
			opAmount, opCurrency,
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
