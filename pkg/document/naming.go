package document

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/osama1998H/moca/pkg/meta"
)

// NamingFunc is the signature for custom naming functions registered with NamingEngine.
// It receives the request context and the document being named, and must return a
// non-empty unique string that becomes the document's primary key.
type NamingFunc func(ctx context.Context, doc Document) (string, error)

// NamingEngine resolves document names according to the MetaType's NamingStrategy.
// All six naming strategies (uuid, field, hash, autoincrement, pattern, custom) are
// supported. NamingEngine is safe for concurrent use.
type NamingEngine struct {
	customFuncs map[string]NamingFunc
	mu          sync.RWMutex
}

// NewNamingEngine returns a NamingEngine with an empty custom function registry.
func NewNamingEngine() *NamingEngine {
	return &NamingEngine{
		customFuncs: make(map[string]NamingFunc),
	}
}

// RegisterNamingFunc registers a custom naming function under name.
// The name must match MetaType.NamingRule.CustomFunc to be selected at runtime.
// Registering the same name twice overwrites the previous function.
// Safe to call concurrently.
func (e *NamingEngine) RegisterNamingFunc(name string, fn NamingFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.customFuncs[name] = fn
}

// GenerateName generates a unique name for doc according to its MetaType's
// NamingStrategy. The pool must be non-nil when the strategy is autoincrement or
// pattern; for all other strategies it is ignored and may be nil.
//
// This function is safe for concurrent use.
func (e *NamingEngine) GenerateName(ctx context.Context, doc Document, pool *pgxpool.Pool) (string, error) {
	strategy := doc.Meta().NamingRule
	tableName := meta.TableName(doc.Meta().Name)

	switch strategy.Rule {
	case meta.NamingUUID, "":
		return e.generateUUID()

	case meta.NamingByField:
		return e.generateByField(doc, strategy.FieldName)

	case meta.NamingByHash:
		return e.generateByHash(doc)

	case meta.NamingAutoIncrement:
		if pool == nil {
			return "", fmt.Errorf("naming: pool required for autoincrement strategy on doctype %q", doc.Meta().Name)
		}
		return e.generateAutoIncrement(ctx, pool, tableName)

	case meta.NamingByPattern:
		// Parse and validate pattern eagerly so callers get a clear error even
		// when pool is nil (e.g., during unit tests).
		prefix, width, parseErr := parsePattern(strategy.Pattern)
		if parseErr != nil {
			return "", fmt.Errorf("naming: invalid pattern %q on doctype %q: %w",
				strategy.Pattern, doc.Meta().Name, parseErr)
		}
		if pool == nil {
			return "", fmt.Errorf("naming: pool required for pattern strategy on doctype %q", doc.Meta().Name)
		}
		return e.generateByPatternResolved(ctx, pool, tableName, prefix, width)

	case meta.NamingCustom:
		return e.generateCustom(ctx, doc, strategy.CustomFunc)

	default:
		return "", fmt.Errorf("naming: unknown rule %q on doctype %q", strategy.Rule, doc.Meta().Name)
	}
}

// generateUUID returns a random UUID v4 string in 8-4-4-4-12 hex format.
func (e *NamingEngine) generateUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("naming: uuid rand.Read: %w", err)
	}
	// Set version 4 bits.
	b[6] = (b[6] & 0x0f) | 0x40
	// Set variant 10xx bits.
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// generateByField uses the value of fieldName from doc as the document name.
// Returns an error if the field is nil or an empty string.
func (e *NamingEngine) generateByField(doc Document, fieldName string) (string, error) {
	v := doc.Get(fieldName)
	if v == nil {
		return "", fmt.Errorf("naming: field %q is nil on doctype %q; cannot use as document name",
			fieldName, doc.Meta().Name)
	}
	s := fmt.Sprintf("%v", v)
	if s == "" {
		return "", fmt.Errorf("naming: field %q is empty on doctype %q; cannot use as document name",
			fieldName, doc.Meta().Name)
	}
	return s, nil
}

// generateByHash returns a 10-character lowercase hex string derived from the
// SHA-256 hash of the doctype name concatenated with all storable scalar field
// values (sorted by field name). Child-table fields are excluded.
func (e *NamingEngine) generateByHash(doc Document) (string, error) {
	mt := doc.Meta()

	// Collect storable scalar field names in sorted order.
	var fieldNames []string
	for _, f := range mt.Fields {
		if !f.FieldType.IsStorable() {
			continue
		}
		// Child-table fields have no scalar representation in this row.
		if f.FieldType == meta.FieldTypeTable || f.FieldType == meta.FieldTypeTableMultiSelect {
			continue
		}
		fieldNames = append(fieldNames, f.Name)
	}
	sort.Strings(fieldNames)

	var sb strings.Builder
	sb.WriteString(mt.Name)
	for _, name := range fieldNames {
		fmt.Fprintf(&sb, "|%s=%v", name, doc.Get(name))
	}

	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])[:10], nil
}

// generateAutoIncrement lazily creates a PG sequence "seq_{tableName}" (idempotent)
// and returns the next value as a decimal string.
//
// Sequence creation uses CREATE SEQUENCE IF NOT EXISTS which is safe under concurrent
// calls because PostgreSQL serialises DDL with exclusive locks.
func (e *NamingEngine) generateAutoIncrement(ctx context.Context, pool *pgxpool.Pool, tableName string) (string, error) {
	seqName := "seq_" + tableName
	quotedSeq := pgx.Identifier{seqName}.Sanitize()

	if _, err := pool.Exec(ctx,
		fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s", quotedSeq)); err != nil {
		return "", fmt.Errorf("naming: create sequence %q: %w", seqName, err)
	}

	var n int64
	if err := pool.QueryRow(ctx,
		fmt.Sprintf("SELECT nextval('%s')", seqName)).Scan(&n); err != nil {
		return "", fmt.Errorf("naming: nextval(%q): %w", seqName, err)
	}
	return fmt.Sprintf("%d", n), nil
}

// generateByPatternResolved generates a name from a pre-parsed pattern.
// It lazily creates the sequence "seq_{tableName}_naming" and formats the result
// as prefix + zero-padded counter of the given width.
func (e *NamingEngine) generateByPatternResolved(ctx context.Context, pool *pgxpool.Pool, tableName, prefix string, width int) (string, error) {
	seqName := "seq_" + tableName + "_naming"
	quotedSeq := pgx.Identifier{seqName}.Sanitize()

	if _, err := pool.Exec(ctx,
		fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s", quotedSeq)); err != nil {
		return "", fmt.Errorf("naming: create sequence %q: %w", seqName, err)
	}

	var n int64
	if err := pool.QueryRow(ctx,
		fmt.Sprintf("SELECT nextval('%s')", seqName)).Scan(&n); err != nil {
		return "", fmt.Errorf("naming: nextval(%q): %w", seqName, err)
	}

	return fmt.Sprintf("%s%0*d", prefix, width, n), nil
}

// generateCustom looks up funcName in the registered NamingFunc registry and
// calls it. Returns an error if no function is registered under funcName.
func (e *NamingEngine) generateCustom(ctx context.Context, doc Document, funcName string) (string, error) {
	e.mu.RLock()
	fn, ok := e.customFuncs[funcName]
	e.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("naming: no custom function registered with name %q for doctype %q",
			funcName, doc.Meta().Name)
	}
	return fn(ctx, doc)
}

// parsePattern parses a naming pattern string into a prefix and counter width.
//
// The pattern must contain exactly one contiguous group of '#' characters,
// which must end the pattern. Everything before the '#' group is the prefix.
// If the character immediately before the '#' group is '.', it is treated as a
// separator and stripped from the prefix:
//
//	"SO-.####" → prefix="SO-",  width=4
//	"INV-####" → prefix="INV-", width=4
//	"#"        → prefix="",     width=1
//
// Errors are returned for: empty pattern, no '#' character, or characters after
// the '#' group.
func parsePattern(pattern string) (prefix string, width int, err error) {
	if pattern == "" {
		return "", 0, fmt.Errorf("pattern is empty")
	}

	firstHash := strings.Index(pattern, "#")
	if firstHash == -1 {
		return "", 0, fmt.Errorf("pattern %q contains no '#' character", pattern)
	}

	prefix = pattern[:firstHash]

	// A '.' immediately before the '#' group is a conventional separator that is
	// stripped from the prefix so the output name does not include it.
	// e.g. "SO-.####" → prefix="SO-" (not "SO-.")
	if len(prefix) > 0 && prefix[len(prefix)-1] == '.' {
		prefix = prefix[:len(prefix)-1]
	}

	// Count the contiguous '#' group.
	i := firstHash
	for i < len(pattern) && pattern[i] == '#' {
		i++
		width++
	}

	// Nothing may follow the '#' group.
	if i < len(pattern) {
		return "", 0, fmt.Errorf("pattern %q has characters after '#' group; '#' characters must end the pattern", pattern)
	}

	return prefix, width, nil
}
