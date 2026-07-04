package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ashep/gnucashsync/internal/config"
	"github.com/ashep/gnucashsync/internal/importer"
	"github.com/ashep/gnucashsync/internal/source"
)

func main() {
	file := flag.String("file", "", "path to .gnucash file (overrides config)")
	cfg := flag.String("config", "", "path to accounts YAML config (default: ~/.gnucashsync.yaml)")
	src := flag.String("source", "", "path to source file (for file-based types)")
	typ := flag.String("type", "", "source type: json, privatbank, monobank")
	dryRun := flag.Bool("dry-run", false, "simulate import without writing to disk")
	flag.Parse()

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

	if *file == "" {
		*file = conf.Book
	}
	if *file == "" {
		fmt.Fprintln(os.Stderr, "error: --file is required (or set 'book' in config)")
		flag.Usage()
		os.Exit(1)
	}

	// Auto-detect type from file extension if not specified.
	if *typ == "" && *src != "" {
		switch strings.ToLower(filepath.Ext(*src)) {
		case ".json":
			*typ = "json"
		case ".csv":
			*typ = "privatbank"
		}
	}

	if *typ == "" {
		*typ = "monobank"
	}

	var s source.Source
	switch *typ {
	case "json":
		if *src == "" {
			log.Fatal("--source is required for type json")
		}
		s = source.NewJSON(*src)
	case "privatbank":
		if *src == "" {
			log.Fatal("--source is required for type privatbank")
		}
		s = source.NewPrivatBank(*src)
	case "monobank":
		s = source.NewMonobank(conf.Sources.Monobank.Token)
	default:
		log.Fatalf("unknown source type %q; valid: json, privatbank, monobank", *typ)
	}

	result, err := importer.Run(s, *file, conf, importer.Options{DryRun: *dryRun})
	if err != nil {
		log.Fatalf("import failed: %v", err)
	}

	if *dryRun {
		for _, t := range result.Transactions {
			desc := t.Description
			if len(desc) > 40 {
				desc = desc[:40]
			}
			fmt.Printf("[dry-run] %s  %-40s  %10s %s\n",
				t.Date.Format("2006-01-02"), desc, t.Amount.StringFixed(2), t.Currency)
		}
		fmt.Printf("[dry-run] Would import: %d, skip duplicates: %d, skip unmapped: %d\n",
			result.Imported, result.SkippedDuplicate, result.SkippedUnmapped)
	} else {
		fmt.Printf("Imported: %d, Skipped (duplicates): %d, Skipped (unmapped): %d\n",
			result.Imported, result.SkippedDuplicate, result.SkippedUnmapped)
	}
}
