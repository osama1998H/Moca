package i18n

import "github.com/osama1998H/moca/pkg/meta"

// Translation represents a single translation entry in the tab_translation table.
type Translation struct {
	SourceText     string `json:"source_text"`
	Language       string `json:"language"`
	TranslatedText string `json:"translated_text"`
	Context        string `json:"context"`
	App            string `json:"app"`
}

// TranslatableString represents a string that can be translated, along with its
// extraction source location for developer reference.
type TranslatableString struct {
	Source  string // the original text to translate
	Context string // translation context, e.g. "DocType:SalesOrder", "label", "option"
	File    string // source file where the string was found
	Line    int    // line number in the source file
}

// TranslationDDL returns the DDL statements for the tab_translation system table
// and its indexes. These are included in GenerateSystemTablesDDL via raw SQL to
// avoid circular imports between pkg/meta and pkg/i18n.
func TranslationDDL() []meta.DDLStatement {
	return []meta.DDLStatement{
		{
			SQL: `CREATE TABLE IF NOT EXISTS tab_translation (
	"source_text"     TEXT NOT NULL,
	"language"        TEXT NOT NULL,
	"translated_text" TEXT NOT NULL,
	"context"         TEXT NOT NULL DEFAULT '',
	"app"             TEXT,
	PRIMARY KEY ("source_text", "language", "context")
)`,
			Comment: "create system table tab_translation",
		},
		{
			SQL:     `CREATE INDEX IF NOT EXISTS idx_translation_app ON tab_translation ("app")`,
			Comment: "create index idx_translation_app on tab_translation",
		},
		{
			SQL:     `CREATE INDEX IF NOT EXISTS idx_translation_lang ON tab_translation ("language")`,
			Comment: "create index idx_translation_lang on tab_translation",
		},
	}
}
