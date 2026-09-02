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
	DryRun        bool
	Since         time.Time // zero means no filter
	Until         time.Time // zero means no filter
	AccountFilter string    // non-empty: only import this source_id
	RateFetcher   func() (map[string]decimal.Decimal, error)
	// HistoricalRateFetcher returns the rates in effect on a given date, keyed
	// "FROM/TO". An empty result means that date has no published rates yet,
	// which is not an error.
	HistoricalRateFetcher func(time.Time) (map[string]decimal.Decimal, error)
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
	rates := &rateResolver{cfg: cfg, opts: opts, triedDates: map[string]bool{}}

	for _, t := range txns {
		if opts.AccountFilter != "" && t.AccountID != opts.AccountFilter {
			continue
		}
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

		counterpart, newDesc, ok := entry.ResolveCounterpart(t.Description, t.Category, t.Amount, cfg.MCCRules)
		if newDesc != "" {
			t.Description = newDesc
		}
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

		// The counterpart split's quantity has to be expressed in the counterpart
		// account's own currency. The bank's operation currency is a different
		// thing: it is whatever currency the bank executed the operation in, and
		// it only happens to coincide most of the time. A tax debited in UAH from
		// a EUR account, for example, is reported by Monobank in UAH while the
		// counterpart expense account is in USD — copying the operation amount
		// there would record hryvnias as dollars.
		var (
			opAmount   decimal.Decimal
			opCurrency string
		)
		if counterpart, ok := gnucash.AccountByGUID(book, creditGUID); ok && counterpart.Currency != t.Currency {
			switch {
			case t.OperationCurrency == counterpart.Currency:
				opAmount, opCurrency = t.OperationAmount, counterpart.Currency
			default:
				converted, err := rates.convert(t.Amount, t.Currency, counterpart.Currency, t.Date)
				if err != nil {
					if !opts.DryRun {
						return Result{}, err
					}
					log.Printf("warning: %v — skipping transaction %q", err, t.ID)
					result.SkippedUnmapped++
					continue
				}
				opAmount, opCurrency = converted.Round(2), counterpart.Currency
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

// rateResolver converts amounts between currencies for one import run, and
// remembers which lookups it has already made so a run costs one request per
// date rather than one per transaction.
type rateResolver struct {
	cfg          *config.Config
	opts         Options
	triedDates   map[string]bool
	triedCurrent bool
}

// convert expresses amount in the `to` currency at the rate that stood on date.
//
// A GnuCash split records what something was worth when it happened, so the
// rate of the transaction's own day is the correct one — using today's rate
// would restate past entries every time they were re-imported. When that day
// has no published rate yet (it is still today, or the rate service is down)
// the current rate is used instead and the substitution is logged.
//
// Note this is only reached when the transaction itself does not already answer
// the question: a bank that reports the operation in the counterpart account's
// own currency has given the exact amount it charged, and that is always used
// in preference to any published rate.
func (r *rateResolver) convert(amount decimal.Decimal, from, to string, date time.Time) (decimal.Decimal, error) {
	if converted, ok := r.cfg.ConvertAmountOn(amount, from, to, date); ok {
		return converted, nil
	}

	if !r.cfg.HasHistoricalRates(date) && !r.triedDates[date.Format(time.DateOnly)] {
		r.triedDates[date.Format(time.DateOnly)] = true

		fetcher := r.opts.HistoricalRateFetcher
		if fetcher == nil {
			fetcher = source.FetchHistoricalRates
		}
		switch fetched, err := fetcher(date); {
		case err != nil:
			log.Printf("warning: could not fetch exchange rates for %s: %v", date.Format(time.DateOnly), err)
		case len(fetched) > 0:
			r.cfg.SetHistoricalRates(date, fetched)
			if err := r.cfg.SaveRates(); err != nil {
				log.Printf("warning: could not save rate cache: %v", err)
			}
			if converted, ok := r.cfg.ConvertAmountOn(amount, from, to, date); ok {
				return converted, nil
			}
		}
	}

	converted, err := r.convertAtCurrentRate(amount, from, to)
	if err != nil {
		return decimal.Zero, err
	}
	log.Printf("warning: no %s/%s rate published for %s; using the current rate instead",
		from, to, date.Format(time.DateOnly))
	return converted, nil
}

// convertAtCurrentRate converts at today's rates, fetching and caching them when
// the cache cannot answer. It returns an error rather than a zero or unconverted
// amount when no rate is available: writing a quantity in the wrong currency
// silently corrupts the ledger.
func (r *rateResolver) convertAtCurrentRate(amount decimal.Decimal, from, to string) (decimal.Decimal, error) {
	if converted, ok := r.cfg.ConvertAmount(amount, from, to); ok {
		return converted, nil
	}
	if r.triedCurrent {
		return decimal.Zero, fmt.Errorf("no exchange rate available for %s/%s", from, to)
	}
	r.triedCurrent = true

	fetcher := r.opts.RateFetcher
	if fetcher == nil {
		fetcher = source.FetchRates
	}
	fetched, err := fetcher()
	if err != nil {
		return decimal.Zero, fmt.Errorf("fetching exchange rates: %w", err)
	}
	for k, v := range fetched {
		// parse "FROM/TO" key and store in config cache
		if len(k) == 7 && k[3] == '/' {
			r.cfg.SetRate(k[:3], k[4:], v)
		}
	}
	if err := r.cfg.SaveRates(); err != nil {
		log.Printf("warning: could not save rate cache: %v", err)
	}

	converted, ok := r.cfg.ConvertAmount(amount, from, to)
	if !ok {
		return decimal.Zero, fmt.Errorf("no exchange rate available for %s/%s", from, to)
	}
	return converted, nil
}
