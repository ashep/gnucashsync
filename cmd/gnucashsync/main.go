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
	file := flag.String("file", "", "path to .gnucash file (required)")
	cfg := flag.String("config", "", "path to accounts YAML config (default: ~/.gnucashsync.yml)")
	src := flag.String("source", "", "path to source file (for file-based types)")
	typ := flag.String("type", "", "source type: json, privatbank, monobank")
	flag.Parse()

	if *file == "" {
		flag.Usage()
		os.Exit(1)
	}

	if *cfg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("resolving home directory: %v", err)
		}
		*cfg = filepath.Join(home, ".gnucashsync.yml")
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
		fmt.Fprintln(os.Stderr, "error: --type is required (json, privatbank, monobank)")
		os.Exit(1)
	}

	conf, err := config.Load(*cfg)
	if err != nil {
		log.Fatalf("loading config: %v", err)
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

	result, err := importer.Run(s, *file, conf)
	if err != nil {
		log.Fatalf("import failed: %v", err)
	}

	fmt.Printf("Imported: %d, Skipped (duplicates): %d, Skipped (unmapped): %d\n",
		result.Imported, result.SkippedDuplicate, result.SkippedUnmapped)
}
