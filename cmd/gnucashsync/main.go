package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ashep/gnucashsync/internal/config"
	"github.com/ashep/gnucashsync/internal/importer"
	"github.com/ashep/gnucashsync/internal/source"
)

func main() {
	file := flag.String("file", "", "path to .gnucash file (overrides config)")
	cfg := flag.String("config", "", "path to accounts YAML config (default: ~/.gnucashsync.yaml)")
	src := flag.String("source", "", "source provider: privatbank, monobank")
	input := flag.String("input", "", "path to source file (for file-based providers)")
	account := flag.String("account", "", "only import from this source_id (default: all accounts)")
	dryRun := flag.Bool("dry-run", false, "simulate import without writing to disk")
	sinceStr := flag.String("since", "", "only import transactions on or after this date (YYYY-MM-DD)")
	untilStr := flag.String("until", "", "only import transactions on or before this date (YYYY-MM-DD)")
	flag.Parse()

	var since time.Time
	if *sinceStr != "" {
		var err error
		since, err = time.ParseInLocation("2006-01-02", *sinceStr, time.Local)
		if err != nil {
			log.Fatalf("invalid --since date %q: expected YYYY-MM-DD", *sinceStr)
		}
	}

	var until time.Time
	if *untilStr != "" {
		var err error
		until, err = time.ParseInLocation("2006-01-02", *untilStr, time.Local)
		if err != nil {
			log.Fatalf("invalid --until date %q: expected YYYY-MM-DD", *untilStr)
		}
		until = until.Add(24*time.Hour - time.Nanosecond) // include the full day
	}

	if !since.IsZero() && !until.IsZero() && since.After(until) {
		log.Fatal("value of -since must not be greater than -until")
	}

	if *cfg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("resolving home directory: %v", err)
		}
		*cfg = filepath.Join(home, ".gnucashsync.yaml")
	}

	conf, err := config.Load(*cfg)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	if *account != "" {
		var filtered []config.AccountEntry
		for _, a := range conf.Accounts {
			if a.SourceID == *account {
				filtered = append(filtered, a)
			}
		}
		if len(filtered) == 0 {
			log.Fatalf("no account with source_id %q found in config", *account)
		}
		conf.Accounts = filtered
	}

	if *file == "" {
		*file = conf.Book
	}
	if *file == "" {
		fmt.Fprintln(os.Stderr, "error: --file is required (or set 'book' in config)")
		flag.Usage()
		os.Exit(1)
	}

	// Auto-detect source provider from input file extension if not specified.
	if *src == "" && *input != "" {
		switch strings.ToLower(filepath.Ext(*input)) {
		case ".xlsx":
			*src = "privatbank"
		}
	}

	if *src == "" && conf.Sources.Privatbank.Dir != "" {
		*src = "privatbank"
	}

	if *src == "" {
		*src = "monobank"
	}

	var s source.Source
	switch *src {
	case "privatbank":
		if *input == "" {
			*input = conf.Sources.Privatbank.Dir
		}
		if *input == "" {
			log.Fatal("--input is required for privatbank source (or set sources.privatbank.dir in config)")
		}
		info, err := os.Stat(*input)
		if err != nil {
			log.Fatalf("privatbank input %q: %v", *input, err)
		}
		if info.IsDir() {
			s = source.NewPrivatBankDir(*input)
		} else if strings.ToLower(filepath.Ext(*input)) == ".xlsx" {
			s = source.NewPrivatBankXLSX(*input)
		} else {
			log.Fatalf("privatbank input %q: expected .xlsx file or directory", *input)
		}
	case "monobank":
		var monobankIDs []string
		for _, a := range conf.Accounts {
			monobankIDs = append(monobankIDs, a.SourceID)
		}
		s = source.NewMonobank(conf.Sources.Monobank.Token, monobankIDs, since, until)
	default:
		log.Fatalf("unknown source %q; valid: privatbank, monobank", *src)
	}

	result, err := importer.Run(s, *file, conf, importer.Options{DryRun: *dryRun, Since: since, Until: until})
	if err != nil {
		log.Fatalf("import failed: %v", err)
	}

	prefix := ""
	if *dryRun {
		prefix = "[dry-run] "
	}

	for _, t := range result.Transactions {
		desc := t.Description
		runes := []rune(desc)
		if len(runes) > 40 {
			desc = string(runes[:40])
		}
		fmt.Printf("%s%s  %-40s  %10s %s\n",
			prefix, t.Date.Format("2006-01-02"), desc, t.Amount.StringFixed(2), t.Currency)
	}

	if *dryRun {
		fmt.Printf("%sWould import: %d, skip duplicates: %d, skip unmapped: %d\n",
			prefix, result.Imported, result.SkippedDuplicate, result.SkippedUnmapped)
	} else {
		fmt.Printf("Imported: %d, Skipped (duplicates): %d, Skipped (unmapped): %d\n",
			result.Imported, result.SkippedDuplicate, result.SkippedUnmapped)
	}
}
