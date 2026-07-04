# Design: SKIP sentinel in description_rules

**Date:** 2026-07-04

## Summary

Allow users to mark transactions as intentionally ignored by specifying `account: SKIP` in a `description_rule`. When a rule with this sentinel matches, the transaction is logged and silently dropped — not imported, not counted as an error.

## Motivation

Currently, if a transaction matches no counterpart rule, the importer either fails (non-dry-run) or logs a warning and increments `SkippedUnmapped`. There is no way to explicitly say "this class of transaction should never be imported." The SKIP sentinel fills that gap.

## Config usage

```yaml
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Monobank UAH"
    description_rules:
      - pattern: "Повернення|Cashback"
        account: SKIP
      - pattern: "SILPO"
        account: "Expenses:Food"
```

Rules are evaluated in order; first match wins. A SKIP rule can appear anywhere in the list.

## Changes

### `internal/config/config.go`

Add one exported constant:

```go
const SkipAccount = "SKIP"
```

`ResolveCounterpart` is unchanged. When a rule with `account: SKIP` matches, it returns `("SKIP", true)` naturally.

### `internal/importer/importer.go`

Add `SkippedRule int` to the `Result` struct.

After `counterpart, ok := entry.ResolveCounterpart(...)`, insert a new branch before the existing unmapped-error block:

```go
if counterpart == config.SkipAccount {
    log.Printf("skipping transaction %q: matched SKIP rule (%s)", t.ID, t.Description)
    result.SkippedRule++
    continue
}
```

### Tests

**`internal/config/config_test.go`**
- `TestResolveCounterpart_SkipRule`: verify that a rule with `account: SKIP` returns `("SKIP", true)`.

**`internal/importer/importer_test.go`**
- `TestRun_SkipsTransactionMatchingSkipRule`: a transaction whose description matches a SKIP rule produces `Imported=0`, `SkippedRule=1`, no error.

## Error handling

No new error conditions. The SKIP sentinel is case-sensitive: `skip` or `Skip` will be treated as a literal account name, not a skip directive. This is intentional — it follows how existing account names work.

## Out of scope

- Case-insensitive SKIP matching
- MCC-level SKIP rules (only `description_rules` for now)
- Exposing `SkippedRule` in CLI output (existing `Result` fields are not printed by the CLI today)
