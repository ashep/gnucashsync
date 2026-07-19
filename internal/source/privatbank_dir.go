package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ashep/gnucashsync/internal/model"
)

type privatBankDirSource struct {
	dir string
}

// NewPrivatBankDir returns a Source that reads all PrivatBank XLSX export files from dir.
func NewPrivatBankDir(dir string) Source {
	return &privatBankDirSource{dir: dir}
}

func (s *privatBankDirSource) Transactions() ([]model.Transaction, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("reading privatbank dir %q: %w", s.dir, err)
	}

	var txns []model.Transaction
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), "~$") {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		if strings.ToLower(filepath.Ext(e.Name())) != ".xlsx" {
			continue
		}
		src := NewPrivatBankXLSX(path)
		t, err := src.Transactions()
		if err != nil {
			return nil, fmt.Errorf("reading %q: %w", path, err)
		}
		txns = append(txns, t...)
	}
	return txns, nil
}
