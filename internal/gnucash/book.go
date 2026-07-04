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

// AccountByGUID returns the ParsedAccount with the given GUID.
func AccountByGUID(book *ParsedBook, guid string) (ParsedAccount, bool) {
	for _, a := range book.Accounts {
		if a.GUID == guid {
			return a, true
		}
	}
	return ParsedAccount{}, false
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
// perspective (negative = money out). operationAmount and operationCurrency
// cover the case where the counterpart account is in a different currency
// (e.g. a UAH account paying for a USD purchase): pass the foreign-currency
// amount with the same sign as amount, and the foreign currency code. When
// operationCurrency is empty or equal to currency the split quantities are
// identical (same-currency behaviour).
func NewTransactionXML(
	sourceID, description, currency string,
	date time.Time,
	amount decimal.Decimal,
	debitGUID, creditGUID string,
	operationAmount decimal.Decimal,
	operationCurrency string,
) string {
	trnGUID := newGUID()
	split1GUID := newGUID()
	split2GUID := newGUID()

	posted := date.UTC().Format("2006-01-02 00:00:00 +0000")
	entered := time.Now().UTC().Format("2006-01-02 15:04:05 +0000")

	debitVal := toRational(amount)
	creditVal := toRational(amount.Neg())

	// creditQty is the quantity in the counterpart account's own currency.
	// For foreign-currency operations it differs from creditVal (which is
	// always expressed in the transaction/home currency).
	creditQty := creditVal
	if operationCurrency != "" && operationCurrency != currency {
		creditQty = toRational(operationAmount.Neg())
	}

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
`, trnGUID, xmlEscape(currency),
		posted, entered,
		xmlEscape(description),
		xmlEscape(sourceID),
		split1GUID, debitVal, debitVal, debitGUID,
		split2GUID, creditVal, creditQty, creditGUID,
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
