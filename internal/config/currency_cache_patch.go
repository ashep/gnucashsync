package config

import (
	"bytes"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// patchCurrencyCacheInFile updates only the currency_cache section of a YAML
// config file, preserving formatting elsewhere.
func patchCurrencyCacheInFile(path string, cache map[string]currencyRateEntry) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	block, err := marshalCurrencyCacheBlock(cache)
	if err != nil {
		return err
	}
	patched, err := replaceCurrencyCacheSection(data, block)
	if err != nil {
		return err
	}
	return os.WriteFile(path, patched, 0600)
}

func marshalCurrencyCacheBlock(cache map[string]currencyRateEntry) ([]byte, error) {
	if len(cache) == 0 {
		return nil, nil
	}
	wrapper := struct {
		CurrencyCache map[string]currencyRateEntry `yaml:"currency_cache"`
	}{CurrencyCache: cache}
	return yaml.Marshal(wrapper)
}

func replaceCurrencyCacheSection(content, block []byte) ([]byte, error) {
	if len(block) > 0 && !bytes.HasSuffix(block, []byte("\n")) {
		block = append(block, '\n')
	}

	lines, trailingNewline := splitConfigLines(content)
	start, comment := findCurrencyCacheSection(lines)

	if start < 0 {
		if len(block) == 0 {
			return content, nil
		}
		block = applyCurrencyCacheComment(block, comment)
		return appendCurrencyCacheSection(content, block), nil
	}

	end := start + 1
	for end < len(lines) && isNestedOrBlankLine(lines[end]) {
		end++
	}

	if len(block) == 0 {
		lines = append(lines[:start], lines[end:]...)
		return joinConfigLines(lines, trailingNewline), nil
	}

	if comment == "" {
		comment = extractCurrencyCacheComment(lines[start])
	}
	block = applyCurrencyCacheComment(block, comment)

	var out []string
	out = append(out, lines[:start]...)
	out = append(out, strings.Split(strings.TrimSuffix(string(block), "\n"), "\n")...)
	out = append(out, lines[end:]...)
	return joinConfigLines(out, trailingNewline), nil
}

func splitConfigLines(content []byte) ([]string, bool) {
	trailingNewline := len(content) > 0 && content[len(content)-1] == '\n'
	s := strings.TrimSuffix(string(content), "\n")
	if s == "" {
		return nil, trailingNewline
	}
	return strings.Split(s, "\n"), trailingNewline
}

func joinConfigLines(lines []string, trailingNewline bool) []byte {
	if len(lines) == 0 {
		if trailingNewline {
			return []byte("\n")
		}
		return nil
	}
	out := strings.Join(lines, "\n")
	if trailingNewline {
		out += "\n"
	}
	return []byte(out)
}

func findCurrencyCacheSection(lines []string) (start int, comment string) {
	for i, line := range lines {
		if isCurrencyCacheLine(line) {
			return i, extractCurrencyCacheComment(line)
		}
	}
	return -1, ""
}

func isCurrencyCacheLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "currency_cache:")
}

func extractCurrencyCacheComment(line string) string {
	idx := strings.Index(line, "#")
	if idx < 0 {
		return ""
	}
	prefix := strings.TrimSpace(line[:idx])
	if !strings.HasPrefix(prefix, "currency_cache:") {
		return ""
	}
	return strings.TrimSpace(line[idx:])
}

func applyCurrencyCacheComment(block []byte, comment string) []byte {
	if comment == "" || len(block) == 0 {
		return block
	}
	lines := strings.SplitN(strings.TrimSuffix(string(block), "\n"), "\n", 2)
	lines[0] = strings.TrimRight(lines[0], " \t") + " " + comment
	if len(lines) == 1 {
		return []byte(lines[0] + "\n")
	}
	return []byte(lines[0] + "\n" + lines[1] + "\n")
}

func isNestedOrBlankLine(line string) bool {
	if line == "" {
		return true
	}
	return line[0] == ' ' || line[0] == '\t'
}

func appendCurrencyCacheSection(content, block []byte) []byte {
	if len(content) == 0 {
		return block
	}
	out := content
	if !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	if !bytes.HasSuffix(out, []byte("\n\n")) {
		out = append(out, '\n')
	}
	return append(out, block...)
}
