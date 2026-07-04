# GnuCashSync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go CLI tool that reads financial transactions from pluggable sources and imports them into a GnuCash `.gnucash` XML file.

**Architecture:** A source adapter reads raw data and produces canonical `Transaction` values. The importer resolves GnuCash account paths via a YAML config, skips duplicates detected via `<trn:slots>` metadata, and appends new transaction XML nodes. The GnuCash layer decompresses the file, scans for accounts and existing source IDs with `xml.Decoder`, surgically inserts new transaction XML at the `</gnc:book>` boundary, and writes atomically with a timestamped backup.

**Tech Stack:** Go 1.22+, `github.com/shopspring/decimal`, `gopkg.in/yaml.v3`, standard library for XML/gzip/HTTP.

## Global Constraints

- Module path: `github.com/ashep/gnucashsync`
- Go version: 1.22 (use `go.mod` `go 1.22`)
- Amount encoding: always `shopspring/decimal`; GnuCash rational format is `numerator/100` (2-decimal currencies)
- GnuCash GUID: 32 lowercase hex characters, generated with `crypto/rand`
- Duplicate detection key: `gnucashsync:source-id` stored in `<trn:slots>`
- Backup count: hardcoded 10
- No test mocking of the file layer — tests use real temp files

---

## File Map

```
cmd/gnucashsync/
  main.go                   CLI entry: flag parsing, wiring, run

internal/
  config/
    config.go               Config struct + YAML load + AccountMapping lookup
    config_test.go

  model/
    transaction.go          Canonical Transaction struct (shared by all sources)

  source/
    source.go               Source interface
    json.go                 JSON file adapter
    json_test.go
    privatbank.go           PrivatBank CSV adapter
    privatbank_test.go
    monobank.go             Monobank API adapter stub

  gnucash/
    namespaces.go           Namespace URI constants + raw XML structs for DecodeElement
    reader.go               Decompress, scan tokens → ParsedBook
    reader_test.go
    book.go                 Account path resolution, transaction XML generation
    book_test.go
    writer.go               Atomic write, backup, LCK detection, count-data update
    writer_test.go

  importer/
    importer.go             Orchestrate source → ParsedBook → write
    importer_test.go

testdata/
  sample.gnucash            Gzip-compressed fixture (created by TestMain)
  transactions.json         Sample JSON transactions fixture
  privatbank.csv            Sample PrivatBank CSV fixture
```

---

## Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `go.sum` (via `go mod tidy`)
- Create: `internal/model/transaction.go`
- Create: `internal/source/source.go`

**Interfaces:**
- Produces: `model.Transaction` struct; `source.Source` interface

- [ ] **Step 1: Initialize the module**

```bash
cd /Users/ashep/src/my/gnucashsync
go mod init github.com/ashep/gnucashsync
```

Expected: `go.mod` created with `module github.com/ashep/gnucashsync` and `go 1.22`.

- [ ] **Step 2: Add dependencies**

```bash
go get github.com/shopspring/decimal
go get gopkg.in/yaml.v3
```

- [ ] **Step 3: Write `internal/model/transaction.go`**

```go
package model

import (
	"time"

	"github.com/shopspring/decimal"
)

type Transaction struct {
	ID          string
	Date        time.Time
	Description string
	Amount      decimal.Decimal
	Currency    string
	AccountID   string
}
```

- [ ] **Step 4: Write `internal/source/source.go`**

```go
package source

import "github.com/ashep/gnucashsync/internal/model"

type Source interface {
	Transactions() ([]model.Transaction, error)
}
```

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/model/transaction.go internal/source/source.go
git commit -m "feat: project scaffold, canonical model, source interface"
```

---

## Task 2: Config

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Load(path string) (*Config, error)`, `config.Config.AccountMapping(sourceID string) (AccountEntry, bool)`

- [ ] **Step 1: Write failing test**

Create `internal/config/config_test.go`:

```go
package config_test

import (
	"os"
	"testing"

	"github.com/ashep/gnucashsync/internal/config"
)

func TestLoad(t *testing.T) {
	yml := `
sources:
  monobank:
    token: "test-token"
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank:Monobank UAH"
    default_counterpart: "Imbalance-UAH"
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
	if entry.DefaultCounterpart != "Imbalance-UAH" {
		t.Errorf("got %q", entry.DefaultCounterpart)
	}

	if cfg.Sources.Monobank.Token != "test-token" {
		t.Errorf("got token %q", cfg.Sources.Monobank.Token)
	}

	_, ok = cfg.AccountMapping("UNKNOWN")
	if ok {
		t.Fatal("expected no mapping for UNKNOWN")
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/config/...
```

Expected: compile error — package `config` does not exist.

- [ ] **Step 3: Write `internal/config/config.go`**

```go
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type MonobankSource struct {
	Token string `yaml:"token"`
}

type Sources struct {
	Monobank MonobankSource `yaml:"monobank"`
}

type AccountEntry struct {
	SourceID           string `yaml:"source_id"`
	GnuCashAccount     string `yaml:"gnucash_account"`
	DefaultCounterpart string `yaml:"default_counterpart"`
}

type Config struct {
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

- [ ] **Step 4: Run test to confirm it passes**

```bash
go test ./internal/config/...
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: config YAML parsing with account mapping lookup"
```

---

## Task 3: GnuCash Namespace Constants & XML Structs

**Files:**
- Create: `internal/gnucash/namespaces.go`

**Interfaces:**
- Produces: namespace URI constants; `rawAccount`, `rawTrn`, `rawSlots`, `rawSlot` structs used by the reader with `xml.DecodeElement`

- [ ] **Step 1: Write `internal/gnucash/namespaces.go`**

No test needed for constants. Write the file:

```go
package gnucash

const (
	nsGnc   = "http://www.gnucash.org/XML/gnc"
	nsAct   = "http://www.gnucash.org/XML/act"
	nsBook  = "http://www.gnucash.org/XML/book"
	nsTrn   = "http://www.gnucash.org/XML/trn"
	nsSplit = "http://www.gnucash.org/XML/split"
	nsTs    = "http://www.gnucash.org/XML/ts"
	nsCmdty = "http://www.gnucash.org/XML/cmdty"
	nsSlot  = "http://www.gnucash.org/XML/slot"
)

// rawAccount is decoded from <gnc:account> elements during the scan pass.
type rawAccount struct {
	Name   string `xml:"http://www.gnucash.org/XML/act name"`
	ID     struct {
		Value string `xml:",chardata"`
	} `xml:"http://www.gnucash.org/XML/act id"`
	Parent *struct {
		Value string `xml:",chardata"`
	} `xml:"http://www.gnucash.org/XML/act parent"`
}

func (a rawAccount) parentGUID() string {
	if a.Parent == nil {
		return ""
	}
	return a.Parent.Value
}

// rawSlot is a single key/value slot.
type rawSlot struct {
	Key   string `xml:"http://www.gnucash.org/XML/slot key"`
	Value string `xml:"http://www.gnucash.org/XML/slot value"`
}

// rawSlots is the <trn:slots> container.
type rawSlots struct {
	Slots []rawSlot `xml:"http://www.gnucash.org/XML/slot slot"`
}

// rawTrn extracts only the slots from a <gnc:transaction> — we don't need
// the full transaction for the scan pass.
type rawTrn struct {
	Slots rawSlots `xml:"http://www.gnucash.org/XML/trn slots"`
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/gnucash/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/gnucash/namespaces.go
git commit -m "feat: GnuCash namespace constants and XML decode structs"
```

---

## Task 4: GnuCash Reader

**Files:**
- Create: `internal/gnucash/reader.go`
- Create: `internal/gnucash/reader_test.go`
- Create: `testdata/` (fixture written in TestMain)

**Interfaces:**
- Produces: `gnucash.ParsedBook` struct; `gnucash.Parse(data []byte) (*ParsedBook, error)`; `gnucash.ReadFile(path string) (*ParsedBook, error)`

- [ ] **Step 1: Write failing test**

Create `internal/gnucash/reader_test.go`:

```go
package gnucash_test

import (
	"compress/gzip"
	"os"
	"testing"

	"github.com/ashep/gnucashsync/internal/gnucash"
)

// sampleXML is a minimal valid GnuCash XML v2 file with:
//   - Root account
//   - Assets account (child of Root)
//   - "Monobank UAH" account (child of Assets)
//   - "Imbalance-UAH" account (child of Root)
//   - One existing transaction tagged with gnucashsync:source-id "existing-001"
const sampleXML = `<?xml version="1.0" encoding="utf-8" ?>
<gnc-v2 xmlns:gnc="http://www.gnucash.org/XML/gnc"
        xmlns:act="http://www.gnucash.org/XML/act"
        xmlns:book="http://www.gnucash.org/XML/book"
        xmlns:cd="http://www.gnucash.org/XML/cd"
        xmlns:cmdty="http://www.gnucash.org/XML/cmdty"
        xmlns:slot="http://www.gnucash.org/XML/slot"
        xmlns:split="http://www.gnucash.org/XML/split"
        xmlns:trn="http://www.gnucash.org/XML/trn"
        xmlns:ts="http://www.gnucash.org/XML/ts">
<gnc:count-data cd:type="book">1</gnc:count-data>
<gnc:book version="2.0.0">
<book:id type="guid">a0000000000000000000000000000000</book:id>
<gnc:count-data cd:type="account">4</gnc:count-data>
<gnc:count-data cd:type="transaction">1</gnc:count-data>
<gnc:account version="2.0.0">
  <act:name>Root Account</act:name>
  <act:id type="guid">a0000000000000000000000000000001</act:id>
  <act:type>ROOT</act:type>
</gnc:account>
<gnc:account version="2.0.0">
  <act:name>Assets</act:name>
  <act:id type="guid">a0000000000000000000000000000002</act:id>
  <act:type>ASSET</act:type>
  <act:commodity><cmdty:space>ISO4217</cmdty:space><cmdty:id>UAH</cmdty:id></act:commodity>
  <act:commodity-scu>100</act:commodity-scu>
  <act:parent type="guid">a0000000000000000000000000000001</act:parent>
</gnc:account>
<gnc:account version="2.0.0">
  <act:name>Monobank UAH</act:name>
  <act:id type="guid">a0000000000000000000000000000003</act:id>
  <act:type>ASSET</act:type>
  <act:commodity><cmdty:space>ISO4217</cmdty:space><cmdty:id>UAH</cmdty:id></act:commodity>
  <act:commodity-scu>100</act:commodity-scu>
  <act:parent type="guid">a0000000000000000000000000000002</act:parent>
</gnc:account>
<gnc:account version="2.0.0">
  <act:name>Imbalance-UAH</act:name>
  <act:id type="guid">a0000000000000000000000000000004</act:id>
  <act:type>INCOME</act:type>
  <act:commodity><cmdty:space>ISO4217</cmdty:space><cmdty:id>UAH</cmdty:id></act:commodity>
  <act:commodity-scu>100</act:commodity-scu>
  <act:parent type="guid">a0000000000000000000000000000001</act:parent>
</gnc:account>
<gnc:transaction version="2.0.0">
  <trn:id type="guid">a0000000000000000000000000000005</trn:id>
  <trn:currency><cmdty:space>ISO4217</cmdty:space><cmdty:id>UAH</cmdty:id></trn:currency>
  <trn:date-posted><ts:date>2026-06-01 00:00:00 +0000</ts:date></trn:date-posted>
  <trn:date-entered><ts:date>2026-06-01 10:00:00 +0000</ts:date></trn:date-entered>
  <trn:description>Existing transaction</trn:description>
  <trn:slots>
    <slot><slot:key>gnucashsync:source-id</slot:key><slot:value type="string">existing-001</slot:value></slot>
  </trn:slots>
  <trn:splits>
    <trn:split>
      <split:id type="guid">a0000000000000000000000000000006</split:id>
      <split:reconciled-state>n</split:reconciled-state>
      <split:value>-10000/100</split:value>
      <split:quantity>-10000/100</split:quantity>
      <split:account type="guid">a0000000000000000000000000000003</split:account>
    </trn:split>
    <trn:split>
      <split:id type="guid">a0000000000000000000000000000007</split:id>
      <split:reconciled-state>n</split:reconciled-state>
      <split:value>10000/100</split:value>
      <split:quantity>10000/100</split:quantity>
      <split:account type="guid">a0000000000000000000000000000004</split:account>
    </trn:split>
  </trn:splits>
</gnc:transaction>
</gnc:book>
</gnc-v2>`

func writeSampleGnuCash(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/sample.gnucash"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte(sampleXML)); err != nil {
		t.Fatal(err)
	}
	gz.Close()
	f.Close()
	return path
}

func TestReadFile_Accounts(t *testing.T) {
	path := writeSampleGnuCash(t)
	book, err := gnucash.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 3 non-root accounts
	if len(book.Accounts) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(book.Accounts))
	}
}

func TestReadFile_SourceIDs(t *testing.T) {
	path := writeSampleGnuCash(t)
	book, err := gnucash.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !book.SourceIDs["existing-001"] {
		t.Error("expected existing-001 in SourceIDs")
	}
}

func TestReadFile_InsertOffset(t *testing.T) {
	path := writeSampleGnuCash(t)
	book, err := gnucash.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if book.InsertOffset <= 0 {
		t.Error("InsertOffset should be > 0")
	}
	// The bytes at InsertOffset should be the start of </gnc:book>
	tail := string(book.Raw[book.InsertOffset:])
	if len(tail) < 11 || tail[:11] != "</gnc:book>" {
		t.Errorf("expected </gnc:book> at InsertOffset, got %q", tail[:20])
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/gnucash/...
```

Expected: compile error — `gnucash.ReadFile` and `gnucash.ParsedBook` undefined.

- [ ] **Step 3: Write `internal/gnucash/reader.go`**

```go
package gnucash

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"io"
	"os"
	"regexp"
	"strconv"
)

// ParsedAccount holds the fields we extract from a <gnc:account> element.
type ParsedAccount struct {
	Name       string
	GUID       string
	ParentGUID string
}

// ParsedBook is the result of scanning a .gnucash file.
// It holds everything the importer needs without fully decoding the XML.
type ParsedBook struct {
	Raw          []byte            // full decompressed bytes
	Accounts     []ParsedAccount   // non-root accounts
	SourceIDs    map[string]bool   // existing gnucashsync:source-id values
	TxnCount     int               // current transaction count from count-data
	InsertOffset int64             // byte offset just before </gnc:book>
}

var txnCountRE = regexp.MustCompile(`<gnc:count-data[^>]*cd:type="transaction"[^>]*>(\d+)</gnc:count-data>`)

// ReadFile decompresses path and calls Parse.
func ReadFile(path string) (*ParsedBook, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse scans the decompressed XML bytes and returns a ParsedBook.
func Parse(data []byte) (*ParsedBook, error) {
	book := &ParsedBook{
		Raw:       data,
		SourceIDs: make(map[string]bool),
	}

	// Extract transaction count with regex — simpler than tracking token positions.
	if m := txnCountRE.FindSubmatch(data); m != nil {
		book.TxnCount, _ = strconv.Atoi(string(m[1]))
	}

	dec := xml.NewDecoder(bytes.NewReader(data))

	for {
		preOffset := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case t.Name.Space == nsGnc && t.Name.Local == "account":
				var a rawAccount
				if err := dec.DecodeElement(&a, &t); err != nil {
					return nil, err
				}
				if a.Name != "Root Account" {
					book.Accounts = append(book.Accounts, ParsedAccount{
						Name:       a.Name,
						GUID:       a.ID.Value,
						ParentGUID: a.parentGUID(),
					})
				}
			case t.Name.Space == nsGnc && t.Name.Local == "transaction":
				var trn rawTrn
				if err := dec.DecodeElement(&trn, &t); err != nil {
					return nil, err
				}
				for _, slot := range trn.Slots.Slots {
					if slot.Key == "gnucashsync:source-id" {
						book.SourceIDs[slot.Value] = true
					}
				}
			}

		case xml.EndElement:
			if t.Name.Space == nsGnc && t.Name.Local == "book" {
				book.InsertOffset = preOffset
			}
		}
	}

	return book, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/gnucash/... -run TestReadFile -v
```

Expected: all three `TestReadFile_*` tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/gnucash/reader.go internal/gnucash/reader_test.go
git commit -m "feat: GnuCash reader — parse accounts, source IDs, insert offset"
```

---

## Task 5: GnuCash Book Operations (Account Path Resolution & Transaction XML)

**Files:**
- Create: `internal/gnucash/book.go`
- Create: `internal/gnucash/book_test.go`

**Interfaces:**
- Produces:
  - `gnucash.AccountPaths(book *ParsedBook) map[string]string` — returns `"path:to:account" → GUID`
  - `gnucash.ResolveAccount(book *ParsedBook, path string) (string, error)` — returns GUID or error
  - `gnucash.NewTransactionXML(id, description, currency string, date time.Time, amount decimal.Decimal, debitGUID, creditGUID string) string`

- [ ] **Step 1: Write failing test**

Create `internal/gnucash/book_test.go`:

```go
package gnucash_test

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/gnucash"
)

func sampleBook(t *testing.T) *gnucash.ParsedBook {
	t.Helper()
	path := writeSampleGnuCash(t) // defined in reader_test.go
	book, err := gnucash.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return book
}

func TestResolveAccount_Found(t *testing.T) {
	book := sampleBook(t)
	guid, err := gnucash.ResolveAccount(book, "Assets:Monobank UAH")
	if err != nil {
		t.Fatal(err)
	}
	if guid != "a0000000000000000000000000000003" {
		t.Errorf("unexpected GUID %q", guid)
	}
}

func TestResolveAccount_NotFound(t *testing.T) {
	book := sampleBook(t)
	_, err := gnucash.ResolveAccount(book, "Assets:Nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown account path")
	}
}

func TestNewTransactionXML_Contains(t *testing.T) {
	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	amount := decimal.NewFromFloat(-450.00)
	xml := gnucash.NewTransactionXML(
		"txn-test-01",
		"Grocery store",
		"UAH",
		date,
		amount,
		"debitguid00000000000000000000001",
		"creditguid0000000000000000000001",
	)

	checks := []string{
		"gnucashsync:source-id",
		"txn-test-01",
		"Grocery store",
		"UAH",
		"2026-07-01",
		"-45000/100",
		"45000/100",
		"debitguid00000000000000000000001",
		"creditguid0000000000000000000001",
	}
	for _, c := range checks {
		if !strings.Contains(xml, c) {
			t.Errorf("transaction XML missing %q\nXML:\n%s", c, xml)
		}
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/gnucash/... -run "TestResolve|TestNewTransaction" -v
```

Expected: compile error — `gnucash.ResolveAccount` and `gnucash.NewTransactionXML` undefined.

- [ ] **Step 3: Write `internal/gnucash/book.go`**

```go
package gnucash

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// AccountPaths computes full colon-separated paths for every non-root account.
// Returns map of "Assets:Bank:Monobank UAH" → GUID.
func AccountPaths(book *ParsedBook) map[string]string {
	byGUID := make(map[string]ParsedAccount, len(book.Accounts))
	for _, a := range book.Accounts {
		byGUID[a.GUID] = a
	}

	paths := make(map[string]string, len(book.Accounts))
	for _, a := range book.Accounts {
		paths[fullPath(a, byGUID)] = a.GUID
	}
	return paths
}

func fullPath(a ParsedAccount, byGUID map[string]ParsedAccount) string {
	if a.ParentGUID == "" {
		return a.Name
	}
	parent, ok := byGUID[a.ParentGUID]
	if !ok {
		return a.Name
	}
	return fullPath(parent, byGUID) + ":" + a.Name
}

// ResolveAccount returns the GUID for the given colon-separated account path.
func ResolveAccount(book *ParsedBook, path string) (string, error) {
	paths := AccountPaths(book)
	guid, ok := paths[path]
	if !ok {
		return "", fmt.Errorf("account %q not found in GnuCash book", path)
	}
	return guid, nil
}

// NewTransactionXML generates a <gnc:transaction> XML fragment ready for
// insertion into a GnuCash XML file. amount is from the bank account's
// perspective (negative = money out).
func NewTransactionXML(
	sourceID, description, currency string,
	date time.Time,
	amount decimal.Decimal,
	debitGUID, creditGUID string,
) string {
	trnGUID := newGUID()
	split1GUID := newGUID()
	split2GUID := newGUID()

	posted := date.UTC().Format("2006-01-02 00:00:00 +0000")
	entered := time.Now().UTC().Format("2006-01-02 15:04:05 +0000")

	debitVal := toRational(amount)
	creditVal := toRational(amount.Neg())

	return fmt.Sprintf(`<gnc:transaction version="2.0.0">
  <trn:id type="guid">%s</trn:id>
  <trn:currency><cmdty:space>ISO4217</cmdty:space><cmdty:id>%s</cmdty:id></trn:currency>
  <trn:date-posted><ts:date>%s</ts:date></trn:date-posted>
  <trn:date-entered><ts:date>%s</ts:date></trn:date-entered>
  <trn:description>%s</trn:description>
  <trn:slots>
    <slot><slot:key>gnucashsync:source-id</slot:key><slot:value type="string">%s</slot:value></slot>
  </trn:slots>
  <trn:splits>
    <trn:split>
      <split:id type="guid">%s</split:id>
      <split:reconciled-state>n</split:reconciled-state>
      <split:value>%s</split:value>
      <split:quantity>%s</split:quantity>
      <split:account type="guid">%s</split:account>
    </trn:split>
    <trn:split>
      <split:id type="guid">%s</split:id>
      <split:reconciled-state>n</split:reconciled-state>
      <split:value>%s</split:value>
      <split:quantity>%s</split:quantity>
      <split:account type="guid">%s</split:account>
    </trn:split>
  </trn:splits>
</gnc:transaction>
`, trnGUID, currency,
		posted, entered,
		xmlEscape(description),
		sourceID,
		split1GUID, debitVal, debitVal, debitGUID,
		split2GUID, creditVal, creditVal, creditGUID,
	)
}

// toRational converts a decimal amount to GnuCash rational format (e.g. -45000/100).
func toRational(d decimal.Decimal) string {
	scaled := d.Mul(decimal.NewFromInt(100)).IntPart()
	return fmt.Sprintf("%d/100", scaled)
}

// newGUID returns a 32-character lowercase hex GUID.
func newGUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", b)
}

// xmlEscape escapes the five XML special characters.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/gnucash/... -run "TestResolve|TestNewTransaction" -v
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/gnucash/book.go internal/gnucash/book_test.go
git commit -m "feat: account path resolution and transaction XML generation"
```

---

## Task 6: GnuCash Writer

**Files:**
- Create: `internal/gnucash/writer.go`
- Create: `internal/gnucash/writer_test.go`

**Interfaces:**
- Produces: `gnucash.Write(book *ParsedBook, txnXMLs []string, destPath string) error`

- [ ] **Step 1: Write failing test**

Create `internal/gnucash/writer_test.go`:

```go
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
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/gnucash/... -run TestWrite -v
```

Expected: compile error — `gnucash.Write` undefined.

- [ ] **Step 3: Write `internal/gnucash/writer.go`**

```go
package gnucash

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxBackups = 10

var txnCountWriteRE = regexp.MustCompile(`(<gnc:count-data[^>]*cd:type="transaction"[^>]*>)\d+(</gnc:count-data>)`)

// Write applies txnXMLs to book.Raw, updates count-data, creates a backup,
// and writes the result atomically to destPath.
func Write(book *ParsedBook, txnXMLs []string, destPath string) error {
	if err := checkLock(destPath); err != nil {
		return err
	}
	if err := backup(destPath); err != nil {
		return err
	}

	data := book.Raw

	// Insert new transactions before </gnc:book>.
	if len(txnXMLs) > 0 {
		insertion := []byte("\n" + strings.Join(txnXMLs, "\n"))
		data = append(data[:book.InsertOffset:book.InsertOffset],
			append(insertion, data[book.InsertOffset:]...)...)
	}

	// Update transaction count.
	newCount := book.TxnCount + len(txnXMLs)
	data = txnCountWriteRE.ReplaceAll(data,
		[]byte("${1}"+strconv.Itoa(newCount)+"${2}"))

	return writeGzip(data, destPath)
}

func checkLock(path string) error {
	if _, err := os.Stat(path + ".LCK"); err == nil {
		return fmt.Errorf("GnuCash book is open (%s.LCK exists); close GnuCash and retry", path)
	}
	return nil
}

func backup(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // nothing to back up on first run
	}
	if err != nil {
		return err
	}

	stamp := time.Now().UTC().Format("20060102T150405")
	bak := path + "." + stamp + ".bak"
	if err := os.WriteFile(bak, data, 0600); err != nil {
		return err
	}

	return pruneBackups(path)
}

func pruneBackups(path string) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var baks []string
	prefix := base + "."
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".bak") {
			baks = append(baks, filepath.Join(dir, e.Name()))
		}
	}

	sort.Strings(baks) // lexicographic = chronological for ISO timestamps
	for len(baks) > maxBackups {
		if err := os.Remove(baks[0]); err != nil {
			return err
		}
		baks = baks[1:]
	}
	return nil
}

func writeGzip(data []byte, destPath string) error {
	tmp := destPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(f)
	buf := bytes.NewReader(data)
	if _, err := buf.WriteTo(gz); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := gz.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, destPath)
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/gnucash/... -run TestWrite -v
```

Expected: all `TestWrite_*` tests pass.

- [ ] **Step 5: Run all gnucash tests**

```bash
go test ./internal/gnucash/... -v
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/gnucash/writer.go internal/gnucash/writer_test.go
git commit -m "feat: GnuCash writer with atomic write, backup, LCK detection"
```

---

## Task 7: JSON Source Adapter

**Files:**
- Create: `internal/source/json.go`
- Create: `internal/source/json_test.go`
- Create: `testdata/transactions.json`

**Interfaces:**
- Consumes: `source.Source` interface (from Task 1)
- Produces: `source.NewJSON(path string) source.Source`

- [ ] **Step 1: Create test fixture**

Create `testdata/transactions.json`:

```json
[
  {
    "id": "txn-001",
    "date": "2026-07-01",
    "description": "Grocery store",
    "amount": -450.00,
    "currency": "UAH",
    "account_id": "UA123"
  },
  {
    "id": "txn-002",
    "date": "2026-07-02",
    "description": "Salary",
    "amount": 50000.00,
    "currency": "UAH",
    "account_id": "UA123"
  }
]
```

- [ ] **Step 2: Write failing test**

Create `internal/source/json_test.go`:

```go
package source_test

import (
	"testing"
	"time"

	"github.com/ashep/gnucashsync/internal/source"
)

func TestJSONSource_Transactions(t *testing.T) {
	s := source.NewJSON("../../testdata/transactions.json")
	txns, err := s.Transactions()
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txns))
	}

	t0 := txns[0]
	if t0.ID != "txn-001" {
		t.Errorf("ID: got %q", t0.ID)
	}
	if t0.Description != "Grocery store" {
		t.Errorf("Description: got %q", t0.Description)
	}
	if t0.Amount.String() != "-450" {
		t.Errorf("Amount: got %s", t0.Amount.String())
	}
	if t0.Currency != "UAH" {
		t.Errorf("Currency: got %q", t0.Currency)
	}
	if t0.AccountID != "UA123" {
		t.Errorf("AccountID: got %q", t0.AccountID)
	}
	if t0.Date != time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("Date: got %v", t0.Date)
	}
}
```

- [ ] **Step 3: Run test to confirm it fails**

```bash
go test ./internal/source/... -run TestJSONSource -v
```

Expected: compile error — `source.NewJSON` undefined.

- [ ] **Step 4: Write `internal/source/json.go`**

```go
package source

import (
	"encoding/json"
	"os"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/model"
)

type jsonRecord struct {
	ID          string          `json:"id"`
	Date        string          `json:"date"`
	Description string          `json:"description"`
	Amount      decimal.Decimal `json:"amount"`
	Currency    string          `json:"currency"`
	AccountID   string          `json:"account_id"`
}

type jsonSource struct {
	path string
}

// NewJSON returns a Source that reads the custom JSON format from path.
func NewJSON(path string) Source {
	return &jsonSource{path: path}
}

func (s *jsonSource) Transactions() ([]model.Transaction, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}

	var records []jsonRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}

	txns := make([]model.Transaction, 0, len(records))
	for _, r := range records {
		date, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			return nil, err
		}
		txns = append(txns, model.Transaction{
			ID:          r.ID,
			Date:        date,
			Description: r.Description,
			Amount:      r.Amount,
			Currency:    r.Currency,
			AccountID:   r.AccountID,
		})
	}
	return txns, nil
}
```

- [ ] **Step 5: Run test to confirm it passes**

```bash
go test ./internal/source/... -run TestJSONSource -v
```

Expected: `PASS`

- [ ] **Step 6: Commit**

```bash
git add internal/source/json.go internal/source/json_test.go testdata/transactions.json
git commit -m "feat: JSON source adapter"
```

---

## Task 8: PrivatBank CSV Adapter

**Files:**
- Create: `internal/source/privatbank.go`
- Create: `internal/source/privatbank_test.go`
- Create: `testdata/privatbank.csv`

**Interfaces:**
- Produces: `source.NewPrivatBank(path string) source.Source`

**Note on format:** PrivatBank CSV uses semicolon separators, comma decimal separators, and `DD.MM.YYYY` dates. Amounts include spaces. The ID is synthesized as `SHA256(date+time+card+amount+description)[:16]` since PrivatBank exports have no native transaction ID.

- [ ] **Step 1: Create test fixture**

Create `testdata/privatbank.csv`:

```
"Дата";"Час";"Категорія";"Картка";"Опис операції";"Сума в валюті картки";"Валюта картки";"Сума у гривні";"Залишок на кінець дня";"Курс"
"01.07.2026";"12:30:00";"Supermarkets";"UA987";"SILPO";" -450,00";"UAH";" -450,00";" 1234,56";"1"
"02.07.2026";"09:00:00";"Income";"UA987";"Salary";" 50000,00";"UAH";" 50000,00";" 51234,56";"1"
```

- [ ] **Step 2: Write failing test**

Create `internal/source/privatbank_test.go`:

```go
package source_test

import (
	"testing"

	"github.com/ashep/gnucashsync/internal/source"
)

func TestPrivatBankSource_Transactions(t *testing.T) {
	s := source.NewPrivatBank("../../testdata/privatbank.csv")
	txns, err := s.Transactions()
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txns))
	}

	t0 := txns[0]
	if t0.AccountID != "UA987" {
		t.Errorf("AccountID: got %q", t0.AccountID)
	}
	if t0.Description != "SILPO" {
		t.Errorf("Description: got %q", t0.Description)
	}
	if t0.Amount.String() != "-450" {
		t.Errorf("Amount: got %s", t0.Amount.String())
	}
	if t0.Currency != "UAH" {
		t.Errorf("Currency: got %q", t0.Currency)
	}
	if t0.ID == "" {
		t.Error("ID should not be empty")
	}
	// IDs must be unique
	if txns[0].ID == txns[1].ID {
		t.Error("IDs should be unique across transactions")
	}
}
```

- [ ] **Step 3: Run test to confirm it fails**

```bash
go test ./internal/source/... -run TestPrivatBank -v
```

Expected: compile error — `source.NewPrivatBank` undefined.

- [ ] **Step 4: Write `internal/source/privatbank.go`**

```go
package source

import (
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/model"
)

type privatBankSource struct {
	path string
}

// NewPrivatBank returns a Source that parses a PrivatBank CSV export.
func NewPrivatBank(path string) Source {
	return &privatBankSource{path: path}
}

func (s *privatBankSource) Transactions() ([]model.Transaction, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comma = ';'
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	// Skip header row.
	if len(records) < 2 {
		return nil, nil
	}

	var txns []model.Transaction
	for _, row := range records[1:] {
		if len(row) < 7 {
			continue
		}
		dateStr := strings.TrimSpace(row[0]) // DD.MM.YYYY
		timeStr := strings.TrimSpace(row[1]) // HH:MM:SS
		card := strings.TrimSpace(row[3])
		description := strings.TrimSpace(row[4])
		amountStr := strings.TrimSpace(row[5])
		currency := strings.TrimSpace(row[6])

		date, err := time.Parse("02.01.2006", dateStr)
		if err != nil {
			return nil, fmt.Errorf("parsing date %q: %w", dateStr, err)
		}

		// Normalize: remove spaces, replace comma decimal separator.
		amountStr = strings.ReplaceAll(amountStr, " ", "")
		amountStr = strings.ReplaceAll(amountStr, ",", ".")
		amount, err := decimal.NewFromString(amountStr)
		if err != nil {
			return nil, fmt.Errorf("parsing amount %q: %w", amountStr, err)
		}

		id := syntheticID(dateStr, timeStr, card, amountStr, description)

		txns = append(txns, model.Transaction{
			ID:          id,
			Date:        date,
			Description: description,
			Amount:      amount,
			Currency:    currency,
			AccountID:   card,
		})
	}
	return txns, nil
}

// syntheticID builds a deterministic 16-char hex ID from transaction fields.
func syntheticID(fields ...string) string {
	h := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return fmt.Sprintf("%x", h[:8])
}
```

- [ ] **Step 5: Run test to confirm it passes**

```bash
go test ./internal/source/... -run TestPrivatBank -v
```

Expected: `PASS`

- [ ] **Step 6: Commit**

```bash
git add internal/source/privatbank.go internal/source/privatbank_test.go testdata/privatbank.csv
git commit -m "feat: PrivatBank CSV source adapter"
```

---

## Task 9: Monobank Adapter Stub

**Files:**
- Create: `internal/source/monobank.go`

**Interfaces:**
- Produces: `source.NewMonobank(token string) source.Source`

- [ ] **Step 1: Write `internal/source/monobank.go`**

```go
package source

import (
	"errors"

	"github.com/ashep/gnucashsync/internal/model"
)

type monobankSource struct {
	token string
}

// NewMonobank returns a Source that fetches transactions from the Monobank API.
// Not yet implemented.
func NewMonobank(token string) Source {
	return &monobankSource{token: token}
}

func (s *monobankSource) Transactions() ([]model.Transaction, error) {
	return nil, errors.New("monobank source not yet implemented")
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/source/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/source/monobank.go
git commit -m "feat: Monobank source stub"
```

---

## Task 10: Importer

**Files:**
- Create: `internal/importer/importer.go`
- Create: `internal/importer/importer_test.go`

**Interfaces:**
- Consumes: `gnucash.ReadFile`, `gnucash.ResolveAccount`, `gnucash.NewTransactionXML`, `gnucash.Write`, `config.Config`, `source.Source`
- Produces: `importer.Result{Imported, SkippedDuplicate, SkippedUnmapped int}`; `importer.Run(src source.Source, gnucashPath string, cfg *config.Config) (Result, error)`

- [ ] **Step 1: Write failing test**

Create `internal/importer/importer_test.go`:

```go
package importer_test

import (
	"compress/gzip"
	"os"
	"testing"

	"github.com/ashep/gnucashsync/internal/config"
	"github.com/ashep/gnucashsync/internal/importer"
	"github.com/ashep/gnucashsync/internal/source"
)

const sampleXML = `<?xml version="1.0" encoding="utf-8" ?>
<gnc-v2 xmlns:gnc="http://www.gnucash.org/XML/gnc"
        xmlns:act="http://www.gnucash.org/XML/act"
        xmlns:book="http://www.gnucash.org/XML/book"
        xmlns:cd="http://www.gnucash.org/XML/cd"
        xmlns:cmdty="http://www.gnucash.org/XML/cmdty"
        xmlns:slot="http://www.gnucash.org/XML/slot"
        xmlns:split="http://www.gnucash.org/XML/split"
        xmlns:trn="http://www.gnucash.org/XML/trn"
        xmlns:ts="http://www.gnucash.org/XML/ts">
<gnc:count-data cd:type="book">1</gnc:count-data>
<gnc:book version="2.0.0">
<book:id type="guid">a0000000000000000000000000000000</book:id>
<gnc:count-data cd:type="account">4</gnc:count-data>
<gnc:count-data cd:type="transaction">0</gnc:count-data>
<gnc:account version="2.0.0">
  <act:name>Root Account</act:name>
  <act:id type="guid">a0000000000000000000000000000001</act:id>
  <act:type>ROOT</act:type>
</gnc:account>
<gnc:account version="2.0.0">
  <act:name>Assets</act:name>
  <act:id type="guid">a0000000000000000000000000000002</act:id>
  <act:type>ASSET</act:type>
  <act:commodity><cmdty:space>ISO4217</cmdty:space><cmdty:id>UAH</cmdty:id></act:commodity>
  <act:commodity-scu>100</act:commodity-scu>
  <act:parent type="guid">a0000000000000000000000000000001</act:parent>
</gnc:account>
<gnc:account version="2.0.0">
  <act:name>Monobank UAH</act:name>
  <act:id type="guid">a0000000000000000000000000000003</act:id>
  <act:type>ASSET</act:type>
  <act:commodity><cmdty:space>ISO4217</cmdty:space><cmdty:id>UAH</cmdty:id></act:commodity>
  <act:commodity-scu>100</act:commodity-scu>
  <act:parent type="guid">a0000000000000000000000000000002</act:parent>
</gnc:account>
<gnc:account version="2.0.0">
  <act:name>Imbalance-UAH</act:name>
  <act:id type="guid">a0000000000000000000000000000004</act:id>
  <act:type>INCOME</act:type>
  <act:commodity><cmdty:space>ISO4217</cmdty:space><cmdty:id>UAH</cmdty:id></act:commodity>
  <act:commodity-scu>100</act:commodity-scu>
  <act:parent type="guid">a0000000000000000000000000000001</act:parent>
</gnc:account>
</gnc:book>
</gnc-v2>`

func writeSampleBook(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/book.gnucash"
	f, _ := os.Create(path)
	gz := gzip.NewWriter(f)
	gz.Write([]byte(sampleXML))
	gz.Close()
	f.Close()
	return path
}

func sampleConfig() *config.Config {
	return &config.Config{
		Accounts: []config.AccountEntry{
			{
				SourceID:           "UA123",
				GnuCashAccount:     "Assets:Monobank UAH",
				DefaultCounterpart: "Imbalance-UAH",
			},
		},
	}
}

func TestRun_ImportsNewTransactions(t *testing.T) {
	path := writeSampleBook(t)
	src := source.NewJSON("../../testdata/transactions.json")
	cfg := sampleConfig()

	result, err := importer.Run(src, path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 2 {
		t.Errorf("expected Imported=2, got %d", result.Imported)
	}
	if result.SkippedDuplicate != 0 {
		t.Errorf("expected SkippedDuplicate=0, got %d", result.SkippedDuplicate)
	}
}

func TestRun_SkipsDuplicates(t *testing.T) {
	path := writeSampleBook(t)
	src := source.NewJSON("../../testdata/transactions.json")
	cfg := sampleConfig()

	// First run.
	importer.Run(src, path, cfg)

	// Second run — same source, should be all duplicates.
	result, err := importer.Run(src, path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 0 {
		t.Errorf("expected Imported=0 on second run, got %d", result.Imported)
	}
	if result.SkippedDuplicate != 2 {
		t.Errorf("expected SkippedDuplicate=2, got %d", result.SkippedDuplicate)
	}
}

func TestRun_SkipsUnmapped(t *testing.T) {
	path := writeSampleBook(t)
	src := source.NewJSON("../../testdata/transactions.json")
	// Config with no matching account — all should be skipped as unmapped.
	cfg := &config.Config{}

	result, err := importer.Run(src, path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedUnmapped != 2 {
		t.Errorf("expected SkippedUnmapped=2, got %d", result.SkippedUnmapped)
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/importer/... -v
```

Expected: compile error — `importer.Run` and `importer.Result` undefined.

- [ ] **Step 3: Write `internal/importer/importer.go`**

```go
package importer

import (
	"fmt"
	"log"

	"github.com/ashep/gnucashsync/internal/config"
	"github.com/ashep/gnucashsync/internal/gnucash"
	"github.com/ashep/gnucashsync/internal/source"
)

// Result summarizes an import run.
type Result struct {
	Imported         int
	SkippedDuplicate int
	SkippedUnmapped  int
}

// Run reads transactions from src, imports new ones into gnucashPath, and
// returns a summary. Account paths must exist in the GnuCash book or the
// function returns an error.
func Run(src source.Source, gnucashPath string, cfg *config.Config) (Result, error) {
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
		result.Imported++
	}

	if err := gnucash.Write(book, txnXMLs, gnucashPath); err != nil {
		return Result{}, fmt.Errorf("writing GnuCash file: %w", err)
	}

	return result, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/importer/... -v
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/importer/
git commit -m "feat: importer orchestration with dedup and unmapped skipping"
```

---

## Task 11: CLI Entry Point

**Files:**
- Create: `cmd/gnucashsync/main.go`

**Interfaces:**
- Consumes: `config.Load`, `source.NewJSON`, `source.NewPrivatBank`, `source.NewMonobank`, `importer.Run`

- [ ] **Step 1: Write `cmd/gnucashsync/main.go`**

```go
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
	cfg := flag.String("config", "", "path to accounts YAML config (required)")
	src := flag.String("source", "", "path to source file (for file-based types)")
	typ := flag.String("type", "", "source type: json, privatbank, monobank")
	flag.Parse()

	if *file == "" || *cfg == "" {
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
```

- [ ] **Step 2: Build and smoke-test**

```bash
go build ./cmd/gnucashsync/
```

Expected: binary `gnucashsync` produced with no errors.

- [ ] **Step 3: Run all tests one final time**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/gnucashsync/main.go
git commit -m "feat: CLI entry point with flag parsing and source selection"
```
