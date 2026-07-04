package gnucash

const (
	nsGnc   = "http://www.gnucash.org/XML/gnc"
	nsAct   = "http://www.gnucash.org/XML/act"
	nsBook  = "http://www.gnucash.org/XML/book"
	nsTrn   = "http://www.gnucash.org/XML/trn"
	nsSplit = "http://www.gnucash.org/XML/split"
	nsTs    = "http://www.gnucash.org/XML/ts"
	nsCmdty = "http://www.gnucash.org/XML/cmdty"
	nsSlot  = "http://www.gnucash.org/XML/slot"
)

// rawAccount is decoded from <gnc:account> elements during the scan pass.
type rawAccount struct {
	Name   string `xml:"http://www.gnucash.org/XML/act name"`
	ID     struct {
		Value string `xml:",chardata"`
	} `xml:"http://www.gnucash.org/XML/act id"`
	Parent *struct {
		Value string `xml:",chardata"`
	} `xml:"http://www.gnucash.org/XML/act parent"`
	Commodity struct {
		ID string `xml:"http://www.gnucash.org/XML/cmdty id"`
	} `xml:"http://www.gnucash.org/XML/act commodity"`
}

func (a rawAccount) parentGUID() string {
	if a.Parent == nil {
		return ""
	}
	return a.Parent.Value
}

// rawSlot is a single key/value slot.
type rawSlot struct {
	Key   string `xml:"http://www.gnucash.org/XML/slot key"`
	Value string `xml:"http://www.gnucash.org/XML/slot value"`
}

// rawSlots is the <trn:slots> container.
// The outer <slot> element carries no namespace prefix in GnuCash XML, so
// the struct tag uses an empty-namespace path.
type rawSlots struct {
	Slots []rawSlot `xml:"slot"`
}

// rawTrn extracts only the slots from a <gnc:transaction> — we don't need
// the full transaction for the scan pass.
type rawTrn struct {
	Slots rawSlots `xml:"http://www.gnucash.org/XML/trn slots"`
}
