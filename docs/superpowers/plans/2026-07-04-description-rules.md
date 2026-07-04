# Description-Rules Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add ordered regexp-based `description_rules` to `AccountEntry` that are checked before `mcc_rules` when resolving the counterpart account for a transaction.

**Architecture:** Two tasks: (1) `config` package gains `DescriptionRule` struct, regexp compilation in `Load`, and `ResolveCounterpart` on `AccountEntry`; (2) importer is updated to call `ResolveCounterpart` and its tests are updated to use the renamed `mcc_rules` field.

**Tech Stack:** Go standard library `regexp`, `gopkg.in/yaml.v3`

## Global Constraints

- No new external dependencies
- Regexp patterns compiled once at `config.Load` time; invalid patterns make `Load` return an error immediately
- Matching is case-sensitive by default; users opt into case-insensitive with `(?i)` prefix
- Existing configs require a one-word rename: `counterparts` → `mcc_rules`

---

### Task 1: Config layer — structs, regexp compilation, ResolveCounterpart

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/importer/importer.go` (one-line change to keep compilation green)

**Interfaces:**
- Produces:
  - `config.DescriptionRule` struct with fields `Pattern string`, `Account string`, and unexported `re *regexp.Regexp`
  - `config.AccountEntry.MCCRules map[string]string` (renamed from `Counterparts`)
  - `func (e *AccountEntry) ResolveCounterpart(description, category string) (string, bool)`

---

- [ ] **Step 1: Write failing tests in `internal/config/config_test.go`**

Replace the entire file with:

```go
package config_test

import (
	"os"
	"testing"

	"github.com/ashep/gnucashsync/internal/config"
)

func loadEntry(t *testing.T, yml, sourceID string) config.AccountEntry {
	t.Helper()
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	f.WriteString(yml)
	f.Close()
	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, ok := cfg.AccountMapping(sourceID)
	if !ok {
		t.Fatalf("no mapping for %q", sourceID)
	}
	return entry
}

func TestLoad(t *testing.T) {
	yml := `
sources:
  monobank:
    token: "test-token"
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank:Monobank UAH"
    mcc_rules:
      "5411": "Imbalance-UAH"
`
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	f.WriteString(yml)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	entry, ok := cfg.AccountMapping("UA123")
	if !ok {
		t.Fatal("expected mapping for UA123")
	}
	if entry.GnuCashAccount != "Assets:Bank:Monobank UAH" {
		t.Errorf("got %q", entry.GnuCashAccount)
	}
	if entry.MCCRules["5411"] != "Imbalance-UAH" {
		t.Errorf("got %q", entry.MCCRules["5411"])
	}
	if cfg.Sources.Monobank.Token != "test-token" {
		t.Errorf("got token %q", cfg.Sources.Monobank.Token)
	}

	_, ok = cfg.AccountMapping("UNKNOWN")
	if ok {
		t.Fatal("expected no mapping for UNKNOWN")
	}
}

func TestLoad_InvalidDescriptionPattern(t *testing.T) {
	yml := `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "["
        account: "Expenses:Food"
`
	f, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	f.WriteString(yml)
	f.Close()

	_, err := config.Load(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid regexp pattern")
	}
}

func TestResolveCounterpart_DescriptionRuleWins(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "SILPO|АТБ"
        account: "Expenses:Food"
    mcc_rules:
      "5411": "Imbalance-UAH"
`, "UA123")

	got, ok := entry.ResolveCounterpart("SILPO supermarket", "5411")
	if !ok {
		t.Fatal("expected match")
	}
	if got != "Expenses:Food" {
		t.Errorf("got %q, want Expenses:Food", got)
	}
}

func TestResolveCounterpart_MCCFallback(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "SILPO"
        account: "Expenses:Food"
    mcc_rules:
      "5411": "Imbalance-UAH"
`, "UA123")

	got, ok := entry.ResolveCounterpart("UBER ride", "5411")
	if !ok {
		t.Fatal("expected match via MCC fallback")
	}
	if got != "Imbalance-UAH" {
		t.Errorf("got %q, want Imbalance-UAH", got)
	}
}

func TestResolveCounterpart_FirstMatchWins(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "SILPO"
        account: "Expenses:Food:Silpo"
      - pattern: "SILPO|АТБ"
        account: "Expenses:Food"
`, "UA123")

	got, ok := entry.ResolveCounterpart("SILPO store", "")
	if !ok {
		t.Fatal("expected match")
	}
	if got != "Expenses:Food:Silpo" {
		t.Errorf("expected first rule to win, got %q", got)
	}
}

func TestResolveCounterpart_NoMatch(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "SILPO"
        account: "Expenses:Food"
    mcc_rules:
      "5411": "Imbalance-UAH"
`, "UA123")

	_, ok := entry.ResolveCounterpart("UNKNOWN store", "9999")
	if ok {
		t.Fatal("expected no match")
	}
}
```

- [ ] **Step 2: Run tests to confirm failures**

```bash
go test ./internal/config/...
```

Expected: compilation errors (`MCCRules` undefined, `ResolveCounterpart` undefined).

- [ ] **Step 3: Implement changes in `internal/config/config.go`**

Replace the entire file with:

```go
package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type MonobankSource struct {
	Token string `yaml:"token"`
}

type Sources struct {
	Monobank MonobankSource `yaml:"monobank"`
}

type DescriptionRule struct {
	Pattern string `yaml:"pattern"`
	Account string `yaml:"account"`
	re      *regexp.Regexp
}

type AccountEntry struct {
	SourceID         string            `yaml:"source_id"`
	GnuCashAccount   string            `yaml:"gnucash_account"`
	DescriptionRules []DescriptionRule `yaml:"description_rules"`
	MCCRules         map[string]string `yaml:"mcc_rules"`
}

// ResolveCounterpart returns the counterpart GnuCash account for a transaction.
// It checks description_rules first (first match wins), then falls back to mcc_rules.
func (e *AccountEntry) ResolveCounterpart(description, category string) (string, bool) {
	for _, r := range e.DescriptionRules {
		if r.re.MatchString(description) {
			return r.Account, true
		}
	}
	account, ok := e.MCCRules[category]
	return account, ok
}

type Config struct {
	Book     string         `yaml:"book"`
	Sources  Sources        `yaml:"sources"`
	Accounts []AccountEntry `yaml:"accounts"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	for i := range cfg.Accounts {
		for j := range cfg.Accounts[i].DescriptionRules {
			r := &cfg.Accounts[i].DescriptionRules[j]
			re, err := regexp.Compile(r.Pattern)
			if err != nil {
				return nil, fmt.Errorf("description_rules: invalid pattern %q: %w", r.Pattern, err)
			}
			r.re = re
		}
	}
	return &cfg, nil
}

func (c *Config) AccountMapping(sourceID string) (AccountEntry, bool) {
	for _, e := range c.Accounts {
		if e.SourceID == sourceID {
			return e, true
		}
	}
	return AccountEntry{}, false
}
```

- [ ] **Step 4: Run config tests to confirm they pass**

```bash
go test ./internal/config/...
```

Expected: all tests PASS.

- [ ] **Step 5: Update the one-line counterpart lookup in `internal/importer/importer.go`**

In `importer.go`, replace:

```go
		counterpart, ok := entry.Counterparts[t.Category]
```

With:

```go
		counterpart, ok := entry.ResolveCounterpart(t.Description, t.Category)
```

- [ ] **Step 6: Confirm existing importer tests still compile and pass**

```bash
go test ./internal/importer/...
```

Expected: compilation error on `entry.Counterparts` → after your edit: PASS (importer tests use `sampleConfig()` which still has `Counterparts` field — you'll fix that in Task 2, but for now confirm the test at least compiles with the new field name).

> Note: `sampleConfig()` in `importer_test.go` still sets `Counterparts: map[string]string{...}` — this will be a compile error. Fix it now by changing that one line in `importer_test.go` from `Counterparts: map[string]string{"5411": "Imbalance-UAH"}` to `MCCRules: map[string]string{"5411": "Imbalance-UAH"}` so the package compiles. Task 2 will add the full description-rule test.

```bash
go test ./internal/importer/...
```

Expected: all existing importer tests PASS.

- [ ] **Step 7: Run full test suite**

```bash
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/importer/importer.go internal/importer/importer_test.go
git commit -m "feat: add description_rules; rename counterparts to mcc_rules"
```

---

### Task 2: Importer — description-rule integration test

**Files:**
- Modify: `internal/importer/importer_test.go`

**Interfaces:**
- Consumes: `config.AccountEntry.MCCRules map[string]string`, `func (e *AccountEntry) ResolveCounterpart(description, category string) (string, bool)` (from Task 1)

---

- [ ] **Step 1: Add the description-rule integration test to `internal/importer/importer_test.go`**

Append this function to the file (after the last existing test):

```go
func TestRun_DescriptionRuleOverridesMCC(t *testing.T) {
	path := writeSampleBook(t)

	// Single transaction that matches the description rule "Grocery".
	// The config has NO mcc_rules, so if the description rule doesn't fire
	// the importer would return an error — a passing test proves it fired.
	txnFile, _ := os.CreateTemp(t.TempDir(), "txns*.json")
	txnFile.WriteString(`[{"id":"txn-desc","date":"2026-07-01","description":"Grocery store","amount":-450.00,"currency":"UAH","account_id":"UA123","category":"5411"}]`)
	txnFile.Close()

	cfgFile, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	cfgFile.WriteString(`
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Monobank UAH"
    description_rules:
      - pattern: "Grocery"
        account: "Imbalance-UAH"
`)
	cfgFile.Close()

	cfg, err := config.Load(cfgFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	result, err := importer.Run(source.NewJSON(txnFile.Name()), path, cfg, importer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 {
		t.Errorf("expected Imported=1, got %d", result.Imported)
	}
}
```

Also add `"os"` to the import block if it isn't already there (it isn't — check the existing imports and add it):

The import block in `importer_test.go` should become:

```go
import (
	"compress/gzip"
	"os"
	"testing"

	"github.com/ashep/gnucashsync/internal/config"
	"github.com/ashep/gnucashsync/internal/importer"
	"github.com/ashep/gnucashsync/internal/source"
)
```

- [ ] **Step 2: Run the new test to confirm it fails before implementation** (it should already pass since Task 1 is done — if it passes, great; if not, investigate)

```bash
go test ./internal/importer/... -run TestRun_DescriptionRuleOverridesMCC -v
```

Expected: PASS (Task 1 already wired up `ResolveCounterpart` in the importer).

- [ ] **Step 3: Run full test suite**

```bash
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/importer/importer_test.go
git commit -m "test: add importer integration test for description_rules"
```
