package config

import (
	"bytes"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// patchRateSectionsInFile updates only the rate-bearing sections of a YAML config
// file, preserving formatting elsewhere.
func patchRateSectionsInFile(path string, cache map[string]currencyRateEntry, history map[string]map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	cacheBlock, err := marshalSectionBlock("currency_cache", cache)
	if err != nil {
		return err
	}
	data, err = replaceSection(data, "currency_cache", cacheBlock)
	if err != nil {
		return err
	}

	historyBlock, err := marshalSectionBlock("rate_history", history)
	if err != nil {
		return err
	}
	data, err = replaceSection(data, "rate_history", historyBlock)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// marshalSectionBlock renders a single top-level mapping as a YAML block, or
// nothing at all when it is empty so the section is dropped from the file.
func marshalSectionBlock(name string, value any) ([]byte, error) {
	node := yaml.Node{Kind: yaml.MappingNode}
	valueNode := &yaml.Node{}
	if err := valueNode.Encode(value); err != nil {
		return nil, err
	}
	if len(valueNode.Content) == 0 {
		return nil, nil
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: name},
		valueNode,
	)
	return yaml.Marshal(&node)
}

func replaceSection(content []byte, name string, block []byte) ([]byte, error) {
	if len(block) > 0 && !bytes.HasSuffix(block, []byte("\n")) {
		block = append(block, '\n')
	}

	lines, trailingNewline := splitConfigLines(content)
	start, comment := findSection(lines, name)

	if start < 0 {
		if len(block) == 0 {
			return content, nil
		}
		block = applySectionComment(block, comment)
		return appendSection(content, block), nil
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
		comment = extractSectionComment(lines[start], name)
	}
	block = applySectionComment(block, comment)

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

func findSection(lines []string, name string) (start int, comment string) {
	for i, line := range lines {
		if isSectionLine(line, name) {
			return i, extractSectionComment(line, name)
		}
	}
	return -1, ""
}

func isSectionLine(line, name string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), name+":")
}

func extractSectionComment(line, name string) string {
	idx := strings.Index(line, "#")
	if idx < 0 {
		return ""
	}
	if !strings.HasPrefix(strings.TrimSpace(line[:idx]), name+":") {
		return ""
	}
	return strings.TrimSpace(line[idx:])
}

func applySectionComment(block []byte, comment string) []byte {
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

func appendSection(content, block []byte) []byte {
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
