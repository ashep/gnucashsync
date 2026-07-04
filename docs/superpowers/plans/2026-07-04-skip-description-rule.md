# SKIP Sentinel in description_rules — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow `account: SKIP` in a `description_rule` to silently drop matching transactions with a log message instead of treating them as unmapped errors.

**Architecture:** Add an exported `SkipAccount` constant to the config package; `ResolveCounterpart` returns it naturally when a SKIP rule matches. The importer checks for the sentinel after resolving the counterpart and branches to log+skip. No existing signatures change.

**Tech Stack:** Go, `log` (stdlib), `regexp` (already used)

## Global Constraints

- Go module: `github.com/ashep/gnucashsync`
- All tests run with: `go test ./...`
- No new dependencies
- SKIP sentinel is case-sensitive: only the exact string `"SKIP"` triggers skip behaviour

---

### Task 1: Add SkipAccount constant and config-level test

**Files:**
- Modify: `internal/config/config.go` — add `const SkipAccount = "SKIP"` (no logic changes)
- Test: `internal/config/config_test.go` — add `TestResolveCounterpart_SkipRule`

**Interfaces:**
- Produces: `config.SkipAccount string` constant consumed by Task 2

- [ ] **Step 1: Write the failing test**

Add this test to `internal/config/config_test.go` after `TestResolveCounterpart_NoMatch`:

```go
func TestResolveCounterpart_SkipRule(t *testing.T) {
	entry := loadEntry(t, `
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Bank"
    description_rules:
      - pattern: "Cashback"
        account: SKIP
`, "UA123")

	got, ok := entry.ResolveCounterpart("Cashback reward", "")
	if !ok {
		t.Fatal("expected match")
	}
	if got != config.SkipAccount {
		t.Errorf("got %q, want %q", got, config.SkipAccount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/config/... -run TestResolveCounterpart_SkipRule -v
```

Expected: FAIL — `config.SkipAccount` undefined.

- [ ] **Step 3: Add the constant to config.go**

In `internal/config/config.go`, add after the `import` block (before the first `type` declaration):

```go
const SkipAccount = "SKIP"
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/config/... -run TestResolveCounterpart_SkipRule -v
```

Expected: PASS

- [ ] **Step 5: Run full test suite to check for regressions**

```bash
go test ./...
```

Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add SkipAccount sentinel constant and test"
```

---

### Task 2: Handle SKIP in importer

**Files:**
- Modify: `internal/importer/importer.go` — add `SkippedRule int` to `Result`; add SKIP branch after counterpart resolution
- Test: `internal/importer/importer_test.go` — add `TestRun_SkipsTransactionMatchingSkipRule`

**Interfaces:**
- Consumes: `config.SkipAccount` from Task 1
- Produces: `Result.SkippedRule int` — count of transactions dropped by SKIP rules

- [ ] **Step 1: Write the failing test**

Add this test to `internal/importer/importer_test.go` after `TestRun_DescriptionRuleOverridesMCC`:

```go
func TestRun_SkipsTransactionMatchingSkipRule(t *testing.T) {
	path := writeSampleBook(t)

	txnFile, _ := os.CreateTemp(t.TempDir(), "txns*.json")
	txnFile.WriteString(`[{"id":"txn-skip","date":"2026-07-01","description":"Cashback reward","amount":50.00,"currency":"UAH","account_id":"UA123","category":"9999"}]`)
	txnFile.Close()

	cfgFile, _ := os.CreateTemp(t.TempDir(), "config*.yaml")
	cfgFile.WriteString(`
accounts:
  - source_id: "UA123"
    gnucash_account: "Assets:Monobank UAH"
    description_rules:
      - pattern: "Cashback"
        account: SKIP
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
	if result.Imported != 0 {
		t.Errorf("expected Imported=0, got %d", result.Imported)
	}
	if result.SkippedRule != 1 {
		t.Errorf("expected SkippedRule=1, got %d", result.SkippedRule)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/importer/... -run TestRun_SkipsTransactionMatchingSkipRule -v
```

Expected: FAIL — `result.SkippedRule` undefined.

- [ ] **Step 3: Add SkippedRule to Result struct**

In `internal/importer/importer.go`, update the `Result` struct:

```go
type Result struct {
	Imported         int
	SkippedDuplicate int
	SkippedUnmapped  int
	SkippedRule      int
	Transactions     []model.Transaction
}
```

- [ ] **Step 4: Add SKIP branch in the import loop**

In `internal/importer/importer.go`, after these two lines:

```go
		counterpart, ok := entry.ResolveCounterpart(t.Description, t.Category)
		if !ok {
```

Insert the SKIP check **before** the `if !ok` block so it reads:

```go
		counterpart, ok := entry.ResolveCounterpart(t.Description, t.Category)
		if ok && counterpart == config.SkipAccount {
			log.Printf("skipping transaction %q: matched SKIP rule (%s)", t.ID, t.Description)
			result.SkippedRule++
			continue
		}
		if !ok {
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./internal/importer/... -run TestRun_SkipsTransactionMatchingSkipRule -v
```

Expected: PASS

- [ ] **Step 6: Run full test suite to check for regressions**

```bash
go test ./...
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/importer/importer.go internal/importer/importer_test.go
git commit -m "feat(importer): skip transactions matching SKIP description rule"
```
