package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/osama1998H/moca/pkg/meta"
)

// tsxPattern matches t("...") and t('...') calls in TSX/TS files.
// Uses a word boundary to avoid matching test(), timeout(), etc.
var tsxPattern = regexp.MustCompile(`\bt\(["']([^"']+)["']\)`)

// templatePattern matches {{ _("text") }} markers in HTML/Go templates.
var templatePattern = regexp.MustCompile(`\{\{\s*_\(["']([^"']+)["']\)\s*\}\}`)

// Extractor extracts translatable strings from MetaType definitions,
// TypeScript/TSX source files, and Go/HTML templates.
type Extractor struct{}

// ExtractFromMetaTypes extracts translatable strings from MetaType definitions:
// MetaType label and description, field labels, select option values,
// and section break labels (via LayoutHint).
func (e *Extractor) ExtractFromMetaTypes(mts []*meta.MetaType) []TranslatableString {
	seen := make(map[string]struct{})
	var result []TranslatableString

	add := func(source, ctx string) {
		source = strings.TrimSpace(source)
		if source == "" {
			return
		}
		key := source + "\x00" + ctx
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, TranslatableString{Source: source, Context: ctx})
	}

	for _, mt := range mts {
		add(mt.Label, "DocType:"+mt.Name)
		add(mt.Description, "DocType:"+mt.Name+":description")

		for _, f := range mt.Fields {
			add(f.Label, "DocType:"+mt.Name+":field:"+f.Name)

			// Select field options (newline-separated).
			if f.FieldType == meta.FieldTypeSelect && f.Options != "" {
				for _, opt := range strings.Split(f.Options, "\n") {
					opt = strings.TrimSpace(opt)
					add(opt, "DocType:"+mt.Name+":option:"+f.Name)
				}
			}

			// SectionBreak labels via LayoutHint.
			if f.FieldType == meta.FieldTypeSectionBreak && f.LayoutHint.Label != "" {
				add(f.LayoutHint.Label, "DocType:"+mt.Name+":section")
			}
		}
	}

	return result
}

// ExtractFromTSX scans a directory for .tsx and .ts files and extracts
// strings from t("...") and t('...') calls. Template literals like
// t(`text ${var}`) are not supported and will not be extracted.
func (e *Extractor) ExtractFromTSX(dir string) ([]TranslatableString, error) {
	seen := make(map[string]struct{})
	var result []TranslatableString

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".tsx" && ext != ".ts" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			matches := tsxPattern.FindAllStringSubmatch(line, -1)
			for _, m := range matches {
				source := m[1]
				if _, ok := seen[source]; ok {
					continue
				}
				seen[source] = struct{}{}
				result = append(result, TranslatableString{
					Source: source,
					File:   path,
					Line:   i + 1,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ExtractFromTemplates scans a directory for .html and .tmpl files
// and extracts strings from {{ _("text") }} markers.
func (e *Extractor) ExtractFromTemplates(dir string) ([]TranslatableString, error) {
	seen := make(map[string]struct{})
	var result []TranslatableString

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".html" && ext != ".tmpl" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			matches := templatePattern.FindAllStringSubmatch(line, -1)
			for _, m := range matches {
				source := m[1]
				if _, ok := seen[source]; ok {
					continue
				}
				seen[source] = struct{}{}
				result = append(result, TranslatableString{
					Source: source,
					File:   path,
					Line:   i + 1,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
