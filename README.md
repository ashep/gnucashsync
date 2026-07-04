# gnucashsync

Import financial transactions from external sources into a GnuCash `.gnucash` file.

Supported sources:

- **Custom JSON** — define your own transaction files
- **PrivatBank CSV** — exports from PrivatBank internet banking
- **Monobank** — API integration (coming soon)

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
gnucashsync --file <book.gnucash> --config <accounts.yaml> [--source <file>] [--type <type>]
```

| Flag       | Required         | Description                                                                                              |
|------------|------------------|----------------------------------------------------------------------------------------------------------|
| `--file`   | yes              | Path to your `.gnucash` file                                                                             |
| `--config` | no               | Path to your account mapping config (YAML). Defaults to `~/.gnucashsync.yml`.                           |
| `--source` | for file sources | Path to the input file (JSON or CSV)                                                                     |
| `--type`   | usually          | Source type: `json`, `privatbank`, `monobank`. Auto-detected from file extension for `.json` and `.csv`. |

**Examples:**

```bash
# Import from a custom JSON file
gnucashsync --file ~/finances/mybook.gnucash --config accounts.yaml --source transactions.json

# Import a PrivatBank CSV export (type auto-detected from .csv extension)
gnucashsync --file ~/finances/mybook.gnucash --config accounts.yaml --source export.csv

# Import from Monobank API (token in config file)
gnucashsync --file ~/finances/mybook.gnucash --config accounts.yaml --type monobank
```

**Output:**

```
Imported: 12, Skipped (duplicates): 3, Skipped (unmapped): 1
```

## Config file

The config file maps source account identifiers to GnuCash account paths and sets the default counterpart account for
each.

```yaml
# Source-specific settings
sources:
  monobank:
    token: "your-monobank-api-token"

# Account mappings
accounts:
  - source_id: "UA123456789"          # card/account number from the source
    gnucash_account: "Assets:Bank:Monobank UAH"   # colon-separated GnuCash path
    default_counterpart: "Imbalance-UAH"          # second split of the double-entry

  - source_id: "UA987654321"
    gnucash_account: "Assets:Bank:PrivatBank UAH"
    default_counterpart: "Imbalance-UAH"
```

**`source_id`** is the account identifier as it appears in your source data — the card number, account ID, or any string
you choose. It must match the `account_id` field in JSON sources or the card column in PrivatBank CSV.

**`gnucash_account`** is the full path to the account in GnuCash, using colons as separators. This must exactly match
the account hierarchy in your book (case-sensitive).

**`default_counterpart`** is where the other half of each double-entry split goes. New transactions land in this account
so you can re-categorize them manually in GnuCash — a standard workflow for bank imports.

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

Not yet implemented. The `--type monobank` flag is accepted and will return an error until the API integration is
complete. The config key `sources.monobank.token` is reserved for the API token.

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
