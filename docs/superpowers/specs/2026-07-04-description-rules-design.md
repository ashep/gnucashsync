# Description-Based Counterpart Rules Design

**Date:** 2026-07-04

## Overview

Add ordered, regexp-based description rules for resolving counterpart accounts when importing transactions into GnuCash. Description rules take priority over MCC-code rules; if no description rule matches, the importer falls back to the existing MCC lookup. The existing `counterparts` config key is renamed `mcc_rules`.

## Config Format

`AccountEntry` gains a new `description_rules` list. The existing `counterparts` key is renamed to `mcc_rules`. Both sections are optional.

```yaml
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank:Monobank UAH"
    description_rules:
      - pattern: "SILPO|АТБ|METRO"
        account: "Expenses:Food:Groceries"
      - pattern: "UBER|BOLT"
        account: "Expenses:Transport"
    mcc_rules:
      "5411": "Expenses:Food"
      "4111": "Expenses:Transport"
```

Existing config files require a one-word rename: `counterparts` → `mcc_rules`.

## Lookup Logic

Counterpart resolution order for each transaction:

1. Walk `description_rules` in order. If `Description` matches a pattern (`regexp.MatchString`), use that rule's `account`. First match wins.
2. If no description rule matched, look up `Category` in `mcc_rules`.
3. If neither matched, hard-fail with the current error message (unchanged).

Regexp patterns are compiled once at config load time. An invalid pattern causes `config.Load` to return an error immediately, so misconfigured regexps fail fast at startup before any file is touched. Matching is case-sensitive by default; users can prefix a pattern with `(?i)` for case-insensitive matching (standard Go regexp syntax).

## Go Struct Changes

### `internal/config/config.go`

New struct:

```go
type DescriptionRule struct {
    Pattern string `yaml:"pattern"`
    Account string `yaml:"account"`
}
```

Updated `AccountEntry`:

```go
type AccountEntry struct {
    SourceID         string            `yaml:"source_id"`
    GnuCashAccount   string            `yaml:"gnucash_account"`
    DescriptionRules []DescriptionRule `yaml:"description_rules"`
    MCCRules         map[string]string `yaml:"mcc_rules"`
}
```

`DescriptionRule` holds a compiled `*regexp.Regexp` as an unexported field (not in the YAML struct) populated during `config.Load`.

New method on `AccountEntry`:

```go
func (e *AccountEntry) ResolveCounterpart(description, category string) (string, bool)
```

Encapsulates the two-step lookup. The importer calls this instead of the current inline `entry.Counterparts[t.Category]` lookup.

### `internal/importer/importer.go`

Replace:

```go
counterpart, ok := entry.Counterparts[t.Category]
```

With:

```go
counterpart, ok := entry.ResolveCounterpart(t.Description, t.Category)
```

## Error Handling

| Situation | Behavior |
|---|---|
| Invalid regexp in `description_rules` | `config.Load` returns error naming the bad pattern; program exits before any file is touched |
| No description or MCC rule matches | Hard-fail with existing error message (unchanged) |

## Testing

- **`config_test.go`**: test `ResolveCounterpart` — description rule match, MCC fallback, no match, first-rule-wins ordering, invalid pattern error from `Load`.
- **`importer_test.go`**: update fixture config to use `mcc_rules` key; add one test where a description rule overrides MCC.
