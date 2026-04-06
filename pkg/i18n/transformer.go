package i18n

import (
	"context"

	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/meta"
)

// I18nTransformer translates MetaType field labels and select options in
// API responses. It implements the api.Transformer interface.
type I18nTransformer struct {
	translator *Translator
}

// NewI18nTransformer creates a transformer that uses the given Translator.
func NewI18nTransformer(t *Translator) *I18nTransformer {
	return &I18nTransformer{translator: t}
}

// TransformRequest is a no-op — translation only applies to responses.
func (t *I18nTransformer) TransformRequest(_ context.Context, _ *meta.MetaType, body map[string]any) (map[string]any, error) {
	return body, nil
}

// TransformResponse translates _meta field labels and select options when
// a language is set in the request context.
func (t *I18nTransformer) TransformResponse(ctx context.Context, mt *meta.MetaType, body map[string]any) (map[string]any, error) {
	lang := api.LanguageFromContext(ctx)
	if lang == "" || mt == nil {
		return body, nil
	}

	site := ""
	if sc := api.SiteFromContext(ctx); sc != nil {
		site = sc.Name
	}
	if site == "" {
		return body, nil
	}

	// Translate _meta section if present.
	metaRaw, ok := body["_meta"]
	if !ok {
		return body, nil
	}
	metaMap, ok := metaRaw.(map[string]any)
	if !ok {
		return body, nil
	}

	// Translate MetaType label.
	if label, ok := metaMap["label"].(string); ok && label != "" {
		metaMap["label"] = t.translator.Translate(ctx, site, lang, label, "DocType:"+mt.Name)
	}

	// Translate field labels and select options.
	if fieldsRaw, ok := metaMap["fields"]; ok {
		if fields, ok := fieldsRaw.([]any); ok {
			for _, fRaw := range fields {
				fMap, ok := fRaw.(map[string]any)
				if !ok {
					continue
				}
				fieldName, _ := fMap["name"].(string)

				if label, ok := fMap["label"].(string); ok && label != "" {
					fMap["label"] = t.translator.Translate(ctx, site, lang, label, "DocType:"+mt.Name+":field:"+fieldName)
				}

				if opts, ok := fMap["options"].(string); ok && opts != "" {
					fMap["options"] = t.translateOptions(ctx, site, lang, mt.Name, fieldName, opts)
				}
			}
		}
	}

	body["_meta"] = metaMap
	return body, nil
}

// translateOptions translates newline-separated select options.
func (t *I18nTransformer) translateOptions(ctx context.Context, site, lang, doctype, fieldName, opts string) string {
	lines := splitOptions(opts)
	for i, line := range lines {
		if line != "" {
			lines[i] = t.translator.Translate(ctx, site, lang, line, "DocType:"+doctype+":option:"+fieldName)
		}
	}
	return joinOptions(lines)
}

func splitOptions(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func joinOptions(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	result := lines[0]
	for _, l := range lines[1:] {
		result += "\n" + l
	}
	return result
}
