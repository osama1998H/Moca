// Package i18n implements the internationalization and translation system for MOCA.
//
// Translations are stored per-tenant in the tab_translation table and cached in
// Redis as hash maps keyed by i18n:{site}:{language}. The system supports string
// extraction from MetaType definitions and source files, import/export in PO, CSV,
// and JSON formats, and compilation to GNU MO binary format.
//
// Key components:
//   - Translator: runtime translation lookup with Redis cache and DB fallback
//   - Extractor: translatable string extraction from MetaTypes, TSX, and templates
//   - Format: PO, CSV, and JSON import/export
//   - Compiler: GNU MO binary format compilation and loading
//   - Middleware: Accept-Language negotiation for HTTP requests
//   - Transformer: API response label translation
package i18n
