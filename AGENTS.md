# gnucashsync

Go CLI that imports bank transactions from external sources into a GnuCash `.gnucash` file. It maps source accounts to GnuCash accounts via YAML config, resolves counterpart (second split) accounts using description/MCC rules, deduplicates by embedded metadata, and appends new transactions with atomic writes and backups.

**Stack:** Go 1.22+, `github.com/shopspring/decimal`, `gopkg.in/yaml.v3`. No external Excel library — PrivatBank XLSX is parsed as ZIP + XML.

## Maintaining this file

After completing any implementation task, review whether `AGENTS.md` needs updating. If you discovered new behavior, changed CLI flags, added sources, altered config schema, established new patterns, or corrected outdated guidance — update this file in the same change set. Keep it accurate and concise; remove or revise sections that no longer apply.

## Repository layout

```
cmd/gnucashsync/          CLI entry point
internal/config/          YAML config, counterpart resolution, exchange-rate cache
internal/source/          Source interface and providers (Monobank, PrivatBank)
internal/importer/        Import orchestration
internal/gnucash/         Read/write GnuCash gzip-XML (lightweight scan, not full decode)
internal/model/           Shared Transaction type
testdata/                 PrivatBank XLSX fixtures for tests
import/privatbank/        Local drop folder for PrivatBank exports (user data; do not commit real statements)
```

## Architecture

```
CLI (main) → config.Load → source.Source.Transactions()
                         → importer.Run → gnucash.ReadFile / Write
```

1. **Source** fetches `[]model.Transaction` from Monobank API or PrivatBank files.
2. **Importer** reads the GnuCash book, sorts transactions, skips duplicates and unmapped accounts, resolves counterpart accounts, builds XML fragments.
3. **gnucash** inserts fragments before `</gnc:book>`, updates `count-data`, validates XML, writes via temp file + rename. Creates timestamped backups (keeps 10).

### `model.Transaction` fields

| Field | Role |
|-------|------|
| `ID` | Stable source ID for duplicate detection (`gnucashsync:source-id` slot) |
| `AccountID` | Maps to config `source_id` (IBAN for Monobank, card number for PrivatBank) |
| `Amount` / `Currency` | From the bank account's perspective (negative = outflow) |
| `OperationAmount` / `OperationCurrency` | Foreign-currency leg when operation currency differs |
| `Category` / `CategoryLabel` | MCC code and human label (Monobank; PrivatBank XLSX category column) |

### Counterpart resolution (per account)

Priority in `config.AccountEntry.ResolveCounterpart`:

1. `description_rules` — regex on description; optional `amount` requires an exact match (AND); optional `new_description` rewrites the transaction description; first match wins
2. Per-account `mcc_rules`
3. Global `mcc_rules`
4. Account `"SKIP"` drops the transaction; no match → error (or warning + skip in `--dry-run`)

## Build and test

```bash
go build ./cmd/gnucashsync/
go test ./...
go test ./internal/source/ -run TestPrivatBank   # single package or test
```

All tests must pass before claiming work is complete. The project vendors nothing in git (`/vendor` is gitignored); dependencies resolve via `go mod`.

## CLI usage

```
gnucashsync --file <book.gnucash> [--config <accounts.yaml>] [--source <provider>] [--input <path>] [options]
```

| Flag | Description |
|------|-------------|
| `--file` | Path to `.gnucash` file (or set `book` in config) |
| `--config` | YAML config path (default: `~/.gnucashsync.yaml`) |
| `--source` | Provider: `privatbank` or `monobank` (default: `monobank`, or `privatbank` if `sources.privatbank.dir` is set) |
| `--input` | File or directory for file-based providers |
| `--account` | Import only this `source_id` or alias |
| `--since` / `--until` | Date filter `YYYY-MM-DD` (inclusive) |
| `--dry-run` | Simulate; no writes |

**PrivatBank input resolution:** `--input` or `sources.privatbank.dir` → directory (`.xlsx` only), or single `.xlsx`. Extension auto-selects provider when `--source` is unset.

**Monobank:** token from `sources.monobank.token`; fetches last 31 days (or `--since`/`--until` range). Only accounts listed in config `accounts` are fetched. Rate-limited responses retry after `Retry-After`.

## Config essentials

```yaml
book: "~/finances/mybook.gnucash"

sources:
  monobank:
    token: "..."
  privatbank:
    dir: "./import/privatbank"   # optional default input directory

mcc_rules:                       # global MCC → GnuCash account
  "5411": "Expenses:Food:Groceries"

accounts:
  - source_id: "UA123456789"      # IBAN (Monobank) or card (PrivatBank)
    alias: "mono_black"           # optional; usable with --account
    gnucash_account: "Assets:Bank:Monobank UAH"
    description_rules:
      - pattern: ".*"
        account: "Imbalance-UAH"
      - pattern: "Netflix"
        amount: "-15.99"           # optional; rule matches only when amount also matches
        new_description: "Netflix" # optional; replaces description when rule matches
        account: "Expenses:Subscriptions"
    mcc_rules: {}                 # optional per-account overrides

currency_cache_ttl: 24h        # optional; default 24h. Go duration string.
currency_cache:                  # auto-populated exchange rates (Monobank API)
  USD/UAH:
    rate: "41.5"
    updated_at: "2026-07-19T10:00:00Z"
```

GnuCash account paths are colon-separated, case-sensitive, and must exist in the book. `config.SaveCurrencyCache()` patches only the `currency_cache` section in the YAML file (preserving user formatting elsewhere). Cached rates expire after `currency_cache_ttl` (default 24h); expired or legacy entries without `updated_at` are re-fetched from Monobank on the next cross-currency import.

## Adding a new source

1. Implement `source.Source` (`Transactions() ([]model.Transaction, error)`) in `internal/source/`.
2. Map fields to `model.Transaction`; ensure stable `ID` values for deduplication.
3. Wire the provider in `cmd/gnucashsync/main.go`.
4. Add tests with fixtures under `testdata/`; use `source.NewSlice` for importer-level tests.

## Coding conventions

- Keep changes minimal and focused; match existing package style.
- Use `decimal.Decimal` for money; never `float64` for amounts.
- Wrap errors with context (`fmt.Errorf("...: %w", err)`).
- `main` uses `log.Fatal`/`log.Fatalf`; library code returns errors; non-fatal issues use `log.Printf`.
- Comments only for non-obvious business logic (e.g. cross-currency split handling).
- Tests: table-driven where appropriate; sample GnuCash XML inline or in test helpers (`internal/gnucash/*_test.go`).

## Safety and sensitive data

- **Never commit:** real `.gnucash` books, bank exports, API tokens, or contents of `import/`.
- Config files may contain `sources.monobank.token` — treat as secrets.
- GnuCash lock files (`.LCK`) block writes intentionally.
- Import is **append-only**; existing transactions/accounts are never modified.
- Duplicate detection uses `gnucashsync:source-id` metadata inside the book — no external state file.

## GnuCash I/O details

- `.gnucash` files are gzip-compressed XML.
- `gnucash.Parse` scans for accounts, existing source IDs, transaction count, and insert offset — it does not fully unmarshal the document.
- `NewTransactionXML` generates double-entry splits; cross-currency transactions set different `value` vs `quantity` on the counterpart split.
- Writer: backup → insert XML → update `count-data` → validate → atomic gzip write.

## PrivatBank formats

| Format | Constructor | `AccountID` | `ID` |
|--------|-------------|-------------|------|
| XLSX | `NewPrivatBankXLSX` | Card column | SHA-256 hash of datetime/card/amount/description |
| Directory | `NewPrivatBankDir` | — | Aggregates `.xlsx` files, skips `~$` temp files |

## Key files for common tasks

| Task | Files |
|------|-------|
| CLI flags / provider wiring | `cmd/gnucashsync/main.go` |
| Import logic / dry-run / cross-currency rates | `internal/importer/importer.go` |
| Config schema / rule resolution | `internal/config/config.go` |
| Monobank API / rate fetch | `internal/source/monobank.go` |
| PrivatBank parsers | `internal/source/privatbank_xlsx.go`, `privatbank_dir.go` |
| GnuCash read/write/backup | `internal/gnucash/reader.go`, `writer.go`, `book.go` |
| MCC code → label lookup | `internal/source/mcc.go` |

## Git / commits

Do not create commits unless explicitly asked. When committing, do not include secrets, real financial data, or user-specific config.

**Co-authored-by trailers:** never add `Co-authored-by` lines for AI assistants or coding agents (including Cursor). Repo hook `.githooks/commit-msg` rejects them. Enable locally once:

```bash
git config core.hooksPath .githooks
```

When committing from Cursor, prefer `git commit-tree` (or amend with a filtered message) if the environment injects a trailer; verify with `git log -1 --format=full` before pushing.
