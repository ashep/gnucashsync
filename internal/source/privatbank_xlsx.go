package source

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/model"
)

type privatBankXLSXSource struct {
	path string
}

// NewPrivatBankXLSX returns a Source that parses a PrivatBank XLSX export.
func NewPrivatBankXLSX(path string) Source {
	return &privatBankXLSXSource{path: path}
}

func (s *privatBankXLSXSource) Transactions() ([]model.Transaction, error) {
	zr, err := zip.OpenReader(s.path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	sharedStrs, err := xlsxSharedStrings(zr)
	if err != nil {
		return nil, err
	}

	rows, err := xlsxRows(zr, sharedStrs)
	if err != nil {
		return nil, err
	}

	// Row 0 is the report title, row 1 is the column header.
	if len(rows) < 3 {
		return nil, nil
	}

	var txns []model.Transaction
	for _, row := range rows[2:] {
		if len(row) < 8 {
			continue
		}

		dateTimeStr := strings.TrimSpace(row[0]) // DD.MM.YYYY HH:MM:SS
		category := strings.TrimSpace(row[1])
		card := strings.TrimSpace(row[2])
		description := strings.TrimSpace(row[3])
		amountStr := strings.TrimSpace(row[4])
		currency := strings.TrimSpace(row[5])
		opAmountStr := strings.TrimSpace(row[6])
		opCurrency := strings.TrimSpace(row[7])

		date, err := time.ParseInLocation("02.01.2006 15:04:05", dateTimeStr, time.Local)
		if err != nil {
			return nil, fmt.Errorf("parsing date %q: %w", dateTimeStr, err)
		}

		amount, err := decimal.NewFromString(amountStr)
		if err != nil {
			return nil, fmt.Errorf("parsing amount %q: %w", amountStr, err)
		}

		var opAmount decimal.Decimal
		if opCurrency != currency {
			opAmount, err = decimal.NewFromString(opAmountStr)
			if err != nil {
				return nil, fmt.Errorf("parsing operation amount %q: %w", opAmountStr, err)
			}
			// The file stores operation amount as unsigned; apply sign from card amount.
			if amount.IsNegative() {
				opAmount = opAmount.Neg()
			}
		} else {
			opCurrency = ""
		}

		id := syntheticID(dateTimeStr, card, amountStr, description)

		txns = append(txns, model.Transaction{
			ID:                id,
			Date:              date,
			Description:       description,
			Amount:            amount,
			Currency:          currency,
			OperationAmount:   opAmount,
			OperationCurrency: opCurrency,
			AccountID:         card,
			Category:          category,
		})
	}

	return txns, nil
}

func xlsxSharedStrings(zr *zip.ReadCloser) ([]string, error) {
	f := xlsxFindFile(zr, "xl/sharedStrings.xml")
	if f == nil {
		return nil, nil
	}

	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var sst struct {
		Si []struct {
			T string `xml:"t"`
			R []struct {
				T string `xml:"t"`
			} `xml:"r"`
		} `xml:"si"`
	}
	if err := xml.NewDecoder(rc).Decode(&sst); err != nil {
		return nil, fmt.Errorf("parsing sharedStrings.xml: %w", err)
	}

	out := make([]string, len(sst.Si))
	for i, si := range sst.Si {
		if si.T != "" {
			out[i] = si.T
		} else {
			var b strings.Builder
			for _, r := range si.R {
				b.WriteString(r.T)
			}
			out[i] = b.String()
		}
	}
	return out, nil
}

func xlsxRows(zr *zip.ReadCloser, sharedStrs []string) ([][]string, error) {
	f := xlsxFindFile(zr, "xl/worksheets/sheet1.xml")
	if f == nil {
		return nil, fmt.Errorf("xl/worksheets/sheet1.xml not found in xlsx")
	}

	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var ws struct {
		SheetData struct {
			Row []struct {
				C []struct {
					R  string `xml:"r,attr"`
					T  string `xml:"t,attr"`
					V  string `xml:"v"`
					Is struct {
						T string `xml:"t"`
					} `xml:"is"`
				} `xml:"c"`
			} `xml:"row"`
		} `xml:"sheetData"`
	}
	if err := xml.NewDecoder(rc).Decode(&ws); err != nil {
		return nil, fmt.Errorf("parsing sheet1.xml: %w", err)
	}

	rows := make([][]string, len(ws.SheetData.Row))
	for i, r := range ws.SheetData.Row {
		maxCol := 0
		for _, c := range r.C {
			if col := xlsxColIndex(c.R); col+1 > maxCol {
				maxCol = col + 1
			}
		}
		row := make([]string, maxCol)
		for _, c := range r.C {
			col := xlsxColIndex(c.R)
			if col < 0 {
				continue
			}
			switch c.T {
			case "s":
				if idx, err := strconv.Atoi(c.V); err == nil && idx >= 0 && idx < len(sharedStrs) {
					row[col] = sharedStrs[idx]
				}
			case "inlineStr":
				row[col] = c.Is.T
			default:
				row[col] = c.V
			}
		}
		rows[i] = row
	}
	return rows, nil
}

func xlsxFindFile(zr *zip.ReadCloser, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// xlsxColIndex converts a cell reference like "A1" or "B12" to a 0-based column index.
func xlsxColIndex(ref string) int {
	col := 0
	for _, c := range ref {
		if c < 'A' || c > 'Z' {
			break
		}
		col = col*26 + int(c-'A'+1)
	}
	return col - 1
}

// syntheticID builds a deterministic 16-char hex ID from transaction fields.
func syntheticID(fields ...string) string {
	h := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return fmt.Sprintf("%x", h[:8])
}
