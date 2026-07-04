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

func TestReadFile_AccountCurrency(t *testing.T) {
	path := writeSampleGnuCash(t)
	book, err := gnucash.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range book.Accounts {
		if a.Name == "Monobank UAH" {
			if a.Currency != "UAH" {
				t.Errorf("expected Currency=UAH, got %q", a.Currency)
			}
			return
		}
	}
	t.Fatal("Monobank UAH account not found")
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
