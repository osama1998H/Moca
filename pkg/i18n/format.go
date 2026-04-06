package i18n

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ── PO format ───────────────────────────────────────────────────────────────

// ExportPO writes translations in GNU gettext PO format.
func ExportPO(translations []Translation, w io.Writer) error {
	bw := bufio.NewWriter(w)

	// PO header.
	write := func(format string, args ...any) {
		_, _ = fmt.Fprintf(bw, format, args...)
	}
	write("# MOCA Translation File\n")
	write("msgid \"\"\n")
	write("msgstr \"\"\n")
	write("\"Content-Type: text/plain; charset=UTF-8\\n\"\n")
	write("\"Content-Transfer-Encoding: 8bit\\n\"\n")
	write("\n")

	for _, t := range translations {
		if t.Context != "" {
			write("msgctxt %s\n", poQuote(t.Context))
		}
		write("msgid %s\n", poQuote(t.SourceText))
		write("msgstr %s\n", poQuote(t.TranslatedText))
		write("\n")
	}

	return bw.Flush()
}

// ImportPO reads translations from a PO-format reader.
func ImportPO(r io.Reader) ([]Translation, error) {
	scanner := bufio.NewScanner(r)
	var translations []Translation
	var current Translation
	var lastField string

	flush := func() {
		if current.SourceText != "" {
			translations = append(translations, current)
		}
		current = Translation{}
		lastField = ""
	}

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Skip comments and blank lines.
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "msgctxt ") {
			current.Context = poUnquote(strings.TrimPrefix(line, "msgctxt "))
			lastField = "msgctxt"
		} else if strings.HasPrefix(line, "msgid ") {
			val := poUnquote(strings.TrimPrefix(line, "msgid "))
			current.SourceText = val
			lastField = "msgid"
		} else if strings.HasPrefix(line, "msgstr ") {
			val := poUnquote(strings.TrimPrefix(line, "msgstr "))
			current.TranslatedText = val
			lastField = "msgstr"
		} else if strings.HasPrefix(line, `"`) {
			// Continuation line for multiline strings.
			val := poUnquote(line)
			switch lastField {
			case "msgctxt":
				current.Context += val
			case "msgid":
				current.SourceText += val
			case "msgstr":
				current.TranslatedText += val
			}
		}
	}
	flush()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("po import: %w", err)
	}

	// Filter out the header entry (empty msgid).
	var filtered []Translation
	for _, t := range translations {
		if t.SourceText != "" {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

// poQuote wraps a string in PO-compatible double quotes with escape sequences.
func poQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}

// poUnquote removes surrounding quotes and unescapes PO escape sequences.
func poUnquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}

// ── CSV format ──────────────────────────────────────────────────────────────

// ExportCSV writes translations as CSV with header:
// source_text,language,translated_text,context,app
func ExportCSV(translations []Translation, w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"source_text", "language", "translated_text", "context", "app"}); err != nil {
		return err
	}
	for _, t := range translations {
		if err := cw.Write([]string{t.SourceText, t.Language, t.TranslatedText, t.Context, t.App}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ImportCSV reads translations from a CSV reader. Expects header row.
func ImportCSV(r io.Reader) ([]Translation, error) {
	cr := csv.NewReader(r)

	// Read and skip header.
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("csv import: read header: %w", err)
	}
	if len(header) < 3 {
		return nil, fmt.Errorf("csv import: expected at least 3 columns, got %d", len(header))
	}

	var translations []Translation
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csv import: %w", err)
		}

		t := Translation{
			SourceText:     safeIndex(record, 0),
			Language:       safeIndex(record, 1),
			TranslatedText: safeIndex(record, 2),
			Context:        safeIndex(record, 3),
			App:            safeIndex(record, 4),
		}
		translations = append(translations, t)
	}
	return translations, nil
}

func safeIndex(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

// ── JSON format ─────────────────────────────────────────────────────────────

// ExportJSON writes translations as a JSON array.
func ExportJSON(translations []Translation, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(translations)
}

// ImportJSON reads translations from a JSON array.
func ImportJSON(r io.Reader) ([]Translation, error) {
	var translations []Translation
	if err := json.NewDecoder(r).Decode(&translations); err != nil {
		return nil, fmt.Errorf("json import: %w", err)
	}
	return translations, nil
}
