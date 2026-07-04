package gnucash

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxBackups = 10

var txnCountWriteRE = regexp.MustCompile(`(<gnc:count-data[^>]*cd:type="transaction"[^>]*>)\d+(</gnc:count-data>)`)

// Write applies txnXMLs to book.Raw, updates count-data, creates a backup,
// and writes the result atomically to destPath.
func Write(book *ParsedBook, txnXMLs []string, destPath string) error {
	if err := checkLock(destPath); err != nil {
		return err
	}
	if err := backup(destPath); err != nil {
		return err
	}

	data := book.Raw

	// Insert new transactions before </gnc:book>.
	if len(txnXMLs) > 0 {
		insertion := []byte("\n" + strings.Join(txnXMLs, "\n"))
		data = append(data[:book.InsertOffset:book.InsertOffset],
			append(insertion, data[book.InsertOffset:]...)...)
	}

	// Update transaction count.
	newCount := book.TxnCount + len(txnXMLs)
	data = txnCountWriteRE.ReplaceAll(data,
		[]byte("${1}"+strconv.Itoa(newCount)+"${2}"))

	return writeGzip(data, destPath)
}

func checkLock(path string) error {
	if _, err := os.Stat(path + ".LCK"); err == nil {
		return fmt.Errorf("GnuCash book is open (%s.LCK exists); close GnuCash and retry", path)
	}
	return nil
}

func backup(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // nothing to back up on first run
	}
	if err != nil {
		return err
	}

	// Use nanosecond precision to guarantee unique names even in tight loops.
	stamp := time.Now().UTC().Format("20060102T150405.000000000")
	bak := path + "." + stamp + ".bak"
	if err := os.WriteFile(bak, data, 0600); err != nil {
		return err
	}

	return pruneBackups(path)
}

func pruneBackups(path string) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var baks []string
	prefix := base + "."
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".bak") {
			baks = append(baks, filepath.Join(dir, e.Name()))
		}
	}

	sort.Strings(baks) // lexicographic = chronological for ISO timestamps
	for len(baks) > maxBackups {
		if err := os.Remove(baks[0]); err != nil {
			return err
		}
		baks = baks[1:]
	}
	return nil
}

func writeGzip(data []byte, destPath string) error {
	tmp := destPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(f)
	buf := bytes.NewReader(data)
	if _, err := buf.WriteTo(gz); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := gz.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, destPath)
}
