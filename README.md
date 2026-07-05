# gnucashsync

Import financial transactions from external sources into a GnuCash `.gnucash` file.

Supported sources:

- **Custom JSON** — define your own transaction files
- **PrivatBank CSV** — exports from PrivatBank internet banking
- **Monobank** — API integration

## How it works

gnucashsync reads transactions from a source, maps them to GnuCash accounts using a YAML config file, and appends new
transactions to your `.gnucash` file. It tracks which transactions have already been imported, so running it multiple
times on the same data is safe — duplicates are skipped automatically.

Each run creates a timestamped backup of your `.gnucash` file. The 10 most recent backups are kept.

## Installation

```bash
go install github.com/ashep/gnucashsync/cmd/gnucashsync@latest
```

Or build from source:

```bash
git clone https://github.com/ashep/gnucashsync
cd gnucashsync
go build ./cmd/gnucashsync/
```

## Usage

```
gnucashsync --file <book.gnucash> --config <accounts.yaml> [--source <file>] [--type <type>] [options]
```

| Flag          | Required         | Description                                                                                              |
|---------------|------------------|----------------------------------------------------------------------------------------------------------|
| `--file`      | yes*             | Path to your `.gnucash` file. Can be set via the `book` key in the config instead.                       |
| `--config`    | no               | Path to your account mapping config (YAML). Defaults to `~/.gnucashsync.yaml`.                           |
| `--source`    | for file sources | Path to the input file (JSON or CSV)                                                                     |
| `--type`      | no               | Source type: `json`, `privatbank`, `monobank`. Auto-detected from `.json`/`.csv` extension; defaults to `monobank`. |
| `--account`   | no               | Only import from this `source_id` (default: all accounts).                                               |
| `--since`     | no               | Only import transactions on or after this date (`YYYY-MM-DD`).                                           |
| `--until`     | no               | Only import transactions on or before this date (`YYYY-MM-DD`).                                          |
| `--dry-run`   | no               | Simulate import without writing to disk; prints what would be imported.                                  |

**Examples:**

```bash
# Import from a custom JSON file
gnucashsync --file ~/finances/mybook.gnucash --config accounts.yaml --source transactions.json

# Import a PrivatBank CSV export (type auto-detected from .csv extension)
gnucashsync --file ~/finances/mybook.gnucash --config accounts.yaml --source export.csv

# Import from Monobank API (token in config file)
gnucashsync --file ~/finances/mybook.gnucash --config accounts.yaml --type monobank

# Preview what would be imported without touching the file
gnucashsync --type monobank --dry-run

# Import only one account, within a specific date range
gnucashsync --type monobank --account UA123456789 --since 2026-06-01 --until 2026-06-30
```

**Output:**

Each imported transaction is printed followed by a summary line:

```
2026-07-01  Grocery store                             -450.00 UAH
2026-07-02  Salary                                  50000.00 UAH
Imported: 2, Skipped (duplicates): 3, Skipped (unmapped): 1
```

With `--dry-run`, no file is written and the summary uses different wording:

```
[dry-run] 2026-07-01  Grocery store                             -450.00 UAH
[dry-run] Would import: 2, skip duplicates: 3, skip unmapped: 1
```

## Config file

The config file maps source account identifiers to GnuCash accounts and defines rules for determining the counterpart
(second split) of each double-entry transaction.

```yaml
# Path to your .gnucash file (can be overridden with --file on the command line)
book: "~/finances/mybook.gnucash"

# Source-specific settings
sources:
  monobank:
    token: "your-monobank-api-token"

# Global MCC category rules — applied to all accounts when no per-account rule matches.
# Keys are MCC codes (as strings); values are GnuCash account paths.
# Use the special value "SKIP" to silently drop matching transactions.
mcc_rules:
  "5411": "Expenses:Food:Groceries"
  "5812": "Expenses:Food:Restaurants"
  "4111": "Expenses:Transport"

# Account mappings
accounts:
  - source_id: "UA123456789"          # card/account IBAN from the source
    gnucash_account: "Assets:Bank:Monobank UAH"

    # Description-based rules (checked first, in order; first match wins).
    # Patterns are regular expressions matched against the transaction description.
    description_rules:
      - pattern: "Salary"
        account: "Income:Salary"
      - pattern: ".*"              # catch-all fallback
        account: "Imbalance-UAH"

  - source_id: "UA987654321"
    gnucash_account: "Assets:Bank:PrivatBank UAH"

    # Per-account MCC rules override global mcc_rules for this account.
    mcc_rules:
      "5411": "Expenses:Groceries:PrivatBank"

    description_rules:
      - pattern: ".*"
        account: "Imbalance-UAH"
```

**`source_id`** is the account identifier as it appears in your source data — the IBAN for Monobank, the card number for
PrivatBank CSV, or any string you choose for JSON sources. It must match the `account_id` field in JSON sources.

**`gnucash_account`** is the full path to the account in GnuCash, using colons as separators. This must exactly match
the account hierarchy in your book (case-sensitive).

### Counterpart resolution

For each transaction, gnucashsync determines the counterpart account (where the other half of the double-entry split
goes) using this priority order:

1. **`description_rules`** (per-account) — regex patterns matched against the transaction description; first match wins.
2. **`mcc_rules`** (per-account) — keyed by MCC category code.
3. **`mcc_rules`** (global) — fallback when no per-account rule matches.

If no rule matches, gnucashsync reports an error and aborts (or, with `--dry-run`, logs a warning and counts the
transaction as unmapped). A catch-all description rule (`pattern: ".*"`) prevents this:

```yaml
description_rules:
  - pattern: ".*"
    account: "Imbalance-UAH"
```

Setting any rule's account to `"SKIP"` silently drops matching transactions instead of importing them.

## Source formats

### Custom JSON

A JSON array of transaction objects:

```json
[
  {
    "id": "txn-001",
    "date": "2026-07-01",
    "description": "Grocery store",
    "amount": -450.00,
    "currency": "UAH",
    "account_id": "UA123456789"
  },
  {
    "id": "txn-002",
    "date": "2026-07-02",
    "description": "Salary",
    "amount": 50000.00,
    "currency": "UAH",
    "account_id": "UA123456789"
  }
]
```

| Field         | Description                                                                                                                   |
|---------------|-------------------------------------------------------------------------------------------------------------------------------|
| `id`          | Unique identifier for the transaction. Used for duplicate detection — must be stable across runs.                             |
| `date`        | Transaction date in `YYYY-MM-DD` format.                                                                                      |
| `description` | Free-form description shown in GnuCash.                                                                                       |
| `amount`      | Decimal amount. **Negative** = money leaving the account (expense, payment). **Positive** = money arriving (income, deposit). |
| `currency`    | ISO 4217 currency code, e.g. `UAH`, `USD`, `EUR`.                                                                             |
| `account_id`  | Must match a `source_id` in your config file.                                                                                 |

### PrivatBank CSV

Export your statement from PrivatBank internet banking and pass the CSV file directly:

```bash
gnucashsync --file mybook.gnucash --config accounts.yaml --source statement.csv
```

The card number from the CSV is used as `account_id` and must have a matching `source_id` entry in your config. Since
PrivatBank exports have no native transaction ID, gnucashsync generates a stable ID from the date, time, card, amount,
and description — so the same export can be imported multiple times safely.

### Monobank

Set your API token in the config file and run without `--source`:

```bash
gnucashsync --type monobank
```

The token is read from `sources.monobank.token`. gnucashsync fetches the last 31 days of transactions from all
accounts on the token. Use your account IBAN as `source_id` in the account mapping — the IBAN is shown in the
Monobank app under card details.

**Rate limiting:** if the Monobank API returns a rate-limit response, gnucashsync waits the duration indicated by the
server and retries automatically.

## Duplicate detection

gnucashsync stores a `gnucashsync:source-id` key in each imported transaction's metadata inside the `.gnucash` file
itself. On every run it scans existing transactions for this key and skips any that are already present. No external
state file is needed.

## Backup strategy

Before every write, gnucashsync copies your `.gnucash` file to a timestamped backup:

```
mybook.gnucash.20260704T143022.000000000.bak
```

The 10 most recent backups are kept; older ones are deleted automatically.

## Safety

- **Atomic writes:** the updated file is written to a `.tmp` file first, then renamed over the original. If anything
  goes wrong during the write, the original is untouched.
- **Lock detection:** if GnuCash has your book open (`.LCK` file present), gnucashsync refuses to write and asks you to
  close GnuCash first.
- **XML validation:** the assembled XML is validated for well-formedness before writing. A corrupt transaction fragment
  causes a hard error rather than a silently broken file.
- **Append-only:** gnucashsync only adds new transaction nodes. It never modifies existing transactions, accounts, or
  commodities.

## Account path resolution

GnuCash account paths in the config must match your book's account hierarchy exactly. For an account named "Monobank
UAH" that is a child of "Assets", the path is:

```
Assets:Monobank UAH
```

For deeper nesting (e.g. Assets → Bank → Monobank UAH):

```
Assets:Bank:Monobank UAH
```

Paths are case-sensitive and must match the account names in GnuCash character-for-character.

## Unmapped transactions

If a transaction's `account_id` has no matching `source_id` in the config, gnucashsync logs a warning and skips the
transaction. The final summary counts how many were skipped this way so you know to update your config.

## Building

Requires Go 1.22+.

```bash
go build ./cmd/gnucashsync/
go test ./...
```
