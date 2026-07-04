package gnucash

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"io"
	"os"
	"regexp"
	"strconv"
)

// ParsedAccount holds the fields we extract from a <gnc:account> element.
type ParsedAccount struct {
	Name       string
	GUID       string
	ParentGUID string
}

// ParsedBook is the result of scanning a .gnucash file.
// It holds everything the importer needs without fully decoding the XML.
type ParsedBook struct {
	Raw          []byte          // full decompressed bytes
	Accounts     []ParsedAccount // non-root accounts
	SourceIDs    map[string]bool // existing gnucashsync:source-id values
	TxnCount     int             // current transaction count from count-data
	InsertOffset int64           // byte offset just before </gnc:book>
}

var txnCountRE = regexp.MustCompile(`<gnc:count-data[^>]*cd:type="transaction"[^>]*>(\d+)</gnc:count-data>`)

// ReadFile decompresses path and calls Parse.
func ReadFile(path string) (*ParsedBook, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse scans the decompressed XML bytes and returns a ParsedBook.
func Parse(data []byte) (*ParsedBook, error) {
	book := &ParsedBook{
		Raw:       data,
		SourceIDs: make(map[string]bool),
	}

	// Extract transaction count with regex — simpler than tracking token positions.
	if m := txnCountRE.FindSubmatch(data); m != nil {
		book.TxnCount, _ = strconv.Atoi(string(m[1]))
	}

	dec := xml.NewDecoder(bytes.NewReader(data))

	for {
		preOffset := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case t.Name.Space == nsGnc && t.Name.Local == "account":
				var a rawAccount
				if err := dec.DecodeElement(&a, &t); err != nil {
					return nil, err
				}
				if a.Name != "Root Account" {
					book.Accounts = append(book.Accounts, ParsedAccount{
						Name:       a.Name,
						GUID:       a.ID.Value,
						ParentGUID: a.parentGUID(),
					})
				}
			case t.Name.Space == nsGnc && t.Name.Local == "transaction":
				var trn rawTrn
				if err := dec.DecodeElement(&trn, &t); err != nil {
					return nil, err
				}
				for _, slot := range trn.Slots.Slots {
					if slot.Key == "gnucashsync:source-id" {
						book.SourceIDs[slot.Value] = true
					}
				}
			}

		case xml.EndElement:
			if t.Name.Space == nsGnc && t.Name.Local == "book" {
				book.InsertOffset = preOffset
			}
		}
	}

	return book, nil
}
