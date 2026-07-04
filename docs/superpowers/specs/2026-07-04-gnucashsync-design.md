# GnuCashSync Design

**Date:** 2026-07-04

## Overview

A Go CLI tool that reads financial transactions from pluggable sources (custom JSON, Monobank API, PrivatBank CSV) and imports them into a GnuCash `.gnucash` file (gzip-compressed XML format). Duplicate transactions are detected and skipped automatically. The original file is never modified in place — writes are atomic and backups are kept.

## CLI Usage

```
gnucashsync --file mybook.gnucash --config accounts.yaml --source transactions.json --type json
gnucashsync --file mybook.gnucash --config accounts.yaml --source export.csv --type privatbank
gnucashsync --file mybook.gnucash --config accounts.yaml --type monobank
```

`--type` selects the source adapter. For file-based sources (json, privatbank) it can be auto-detected from the file extension.

## Architecture

The program is a four-stage pipeline:

```
Source  →  Canonical Model  →  Importer  →  GnuCash XML
```

1. A **source adapter** reads raw data and produces a slice of canonical `Transaction` values.
2. The **importer** loads the `.gnucash` file, resolves account paths via the config, skips duplicates, and appends new GnuCash transactions.
3. The **GnuCash XML layer** reads and writes the `.gnucash` file using Go structs that mirror the schema.

## Project Layout

```
cmd/gnucashsync/
  main.go               # flag parsing, orchestration
internal/
  config/
    config.go           # YAML config parsing
  model/
    transaction.go      # canonical Transaction struct
  source/
    source.go           # Source interface
    json.go             # custom JSON adapter
    monobank.go         # Monobank API adapter
    privatbank.go       # PrivatBank CSV adapter
  gnucash/
    xml.go              # GnuCash XML struct definitions
    reader.go           # decompress + parse
    writer.go           # serialize + compress, atomic write, backup
    book.go             # higher-level ops: find account, add transaction, dedup
  importer/
    importer.go         # source → gnucash orchestration
```

## Data Model

### Canonical Transaction

All source adapters produce this struct:

```go
type Transaction struct {
    ID          string          // external ID for deduplication
    Date        time.Time
    Description string
    Amount      decimal.Decimal // negative = debit, positive = credit
    Currency    string          // ISO 4217, e.g. "UAH"
    AccountID   string          // source-side account identifier
}
```

### Custom JSON Source Format

```json
[
  {
    "id": "txn-001",
    "date": "2026-07-01",
    "description": "Grocery store",
    "amount": -450.00,
    "currency": "UAH",
    "account_id": "UA123456789"
  }
]
```

- `amount`: negative for debits (money leaving), positive for credits
- `date`: `YYYY-MM-DD`
- `account_id`: matches a `source_id` in the config file
- `id`: unique per transaction, used for deduplication

## Config File

```yaml
sources:
  monobank:
    token: "your-monobank-api-token"

accounts:
  - source_id: "UA123456789"
    gnucash_account: "Assets:Bank:Monobank UAH"
    default_counterpart: "Imbalance-UAH"
  - source_id: "UA987654321"
    gnucash_account: "Assets:Bank:PrivatBank UAH"
    default_counterpart: "Imbalance-UAH"
```

The optional `sources` section holds source-specific configuration (e.g. API tokens). The `accounts` section maps source-side account identifiers to GnuCash account paths.

`gnucash_account` is the colon-separated GnuCash account path for the primary split. `default_counterpart` is the second split — since bank exports only know one side of a transaction, the counterpart goes to an Imbalance account for the user to re-categorize manually in GnuCash.

## GnuCash XML Layer

The `.gnucash` file is gzip-compressed XML. The layer:

1. **Reads**: decompresses with `compress/gzip`, parses with `encoding/xml` into Go structs
2. **Writes**: serializes to XML, compresses with gzip, writes atomically (temp file → rename)

Key XML structures:

```
Book
  └── Account (tree, GUID-identified, full-name path e.g. "Assets:Bank:Monobank UAH")
  └── Transaction
        ├── <trn:id>          internal GUID (generated fresh on import)
        ├── <trn:date-posted>
        ├── <trn:description>
        ├── <trn:slots>       stores gnucashsync:source-id for dedup
        └── Split × 2
              ├── account GUID ref
              └── amount as rational number (e.g. "45000/100")
```

GnuCash stores amounts as rational numbers (`numerator/denominator`). `github.com/shopspring/decimal` is used for accurate decimal arithmetic and conversion.

The layer only **appends** new transaction nodes — it never modifies existing transactions, accounts, or commodities.

## Duplicate Detection

The external transaction `ID` is stored in `<trn:slots>` under the key `gnucashsync:source-id`. On each import run, existing transactions are scanned for this slot value. Matches are skipped silently (counted in the summary). No separate state file is required — dedup state lives inside the `.gnucash` file.

## Write Safety & Backups

**Atomic write:** the updated file is written to `<filename>.tmp`, then renamed to the original path. The original is never touched until the write fully succeeds.

**Backups:** before any write, the original is copied to a timestamped backup file:

```
mybook.gnucash.20260701T143022.bak
```

After a successful write, backups beyond the 10 most recent are deleted. Backup count is hardcoded to 10.

**LCK file detection:** GnuCash creates a `.LCK` file when the book is open. The program detects this and refuses to write, printing a message asking the user to close GnuCash first.

## Error Handling

| Situation | Behavior |
|---|---|
| Transaction `account_id` not in config | Log warning, skip transaction |
| Account path not found in GnuCash book | Hard fail with message naming the missing path |
| GnuCash book is open (`.LCK` present) | Hard fail with message to close GnuCash |
| Write or backup failure | Hard fail, original file untouched |

## Exit Summary

On completion the program prints:

```
Imported: 12, Skipped (duplicates): 3, Skipped (unmapped): 1
```

## Testing

- **GnuCash XML layer**: unit tests with real fixture `.gnucash` files — round-trip read/write, account path resolution, duplicate detection, amount conversion
- **Source adapters**: unit tests per adapter with sample input fixtures
- **Importer**: integration test using a fixture `.gnucash` file — run import, re-read file, assert correct transactions
- Tests use real files in a temp directory; no mocking of the file layer

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/shopspring/decimal` | Accurate decimal arithmetic, GnuCash rational conversion |
| `gopkg.in/yaml.v3` | Config file parsing |
| Standard library only otherwise | XML, gzip, file I/O, HTTP (for Monobank) |
