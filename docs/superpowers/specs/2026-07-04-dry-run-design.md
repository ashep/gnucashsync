# Dry-Run Mode Design

**Date:** 2026-07-04  
**Project:** gnucashsync

## Overview

Add a `--dry-run` flag that simulates a full import run without writing anything to disk. The user sees exactly which transactions would be imported — with date, description, and amount — plus the usual summary line, all prefixed with `[dry-run]`. No backup is created, no file is modified.

## API Changes

### `importer.Options`

A new `Options` struct passed to `importer.Run`:

```go
type Options struct {
    DryRun bool
}

func Run(src source.Source, gnucashPath string, cfg *config.Config, opts Options) (Result, error)
```

### `importer.Result`

Add `Transactions []model.Transaction` — always populated with the transactions that were (or would be) imported. Useful beyond dry-run for future logging or auditing:

```go
type Result struct {
    Imported         int
    SkippedDuplicate int
    SkippedUnmapped  int
    Transactions     []model.Transaction
}
```

### Dry-run gate

When `opts.DryRun` is true, `importer.Run` skips the `gnucash.Write()` call entirely. No backup is created, no lock check is performed, no file is touched.

## CLI

New flag:

```
--dry-run    simulate the import without writing to disk
```

Example:

```bash
gnucashsync --dry-run --file mybook.gnucash --source transactions.json
```

## Output Format

In dry-run mode, each would-be-imported transaction is printed before the summary:

```
[dry-run] 2026-07-01  Grocery store                    -450.00 UAH
[dry-run] 2026-07-02  Salary                          50000.00 UAH
[dry-run] Would import: 2, skip duplicates: 3, skip unmapped: 0
```

- Each line: `[dry-run]  DATE  DESCRIPTION  AMOUNT CURRENCY`
- Description truncated to 40 characters if longer
- Amounts right-aligned in a fixed-width column
- Summary uses `Would import:` instead of `Imported:`

Normal (non-dry) output is unchanged.

## Files Changed

| File | Change |
|------|--------|
| `internal/importer/importer.go` | Add `Options`, add `Transactions` to `Result`, gate write on `opts.DryRun`, populate `result.Transactions` |
| `cmd/gnucashsync/main.go` | Add `--dry-run` flag, pass `Options` to `Run`, print transaction list and prefixed summary in dry-run mode |
| `internal/importer/importer_test.go` | Add test: dry-run leaves the file unchanged and `Result.Transactions` contains expected entries |

No changes to `gnucash/writer.go`, `internal/model/`, or any source files.
