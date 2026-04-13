package console

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// Console provides helper methods exposed in the yaegi REPL.
type Console struct {
	DocManager *document.DocManager
	Registry   *meta.Registry
	Pool       *pgxpool.Pool
	Site       *tenancy.SiteContext
	User       *auth.User
}

func (c *Console) docCtx() *document.DocContext {
	return document.NewDocContext(context.Background(), c.Site, c.User)
}

// Get fetches a single document by doctype and name, returning it as a plain map.
func (c *Console) Get(doctype, name string) (map[string]any, error) {
	if c.DocManager == nil {
		return nil, fmt.Errorf("console: DocManager not initialized")
	}
	doc, err := c.DocManager.Get(c.docCtx(), doctype, name)
	if err != nil {
		return nil, err
	}
	return doc.AsMap(), nil
}

// GetList fetches a list of documents by doctype, with optional equality filters.
// The first optional argument may be a map[string]any of field->value filters.
// Returns the documents as plain maps, the total count, and any error.
func (c *Console) GetList(doctype string, filters ...any) ([]map[string]any, int, error) {
	if c.DocManager == nil {
		return nil, 0, fmt.Errorf("console: DocManager not initialized")
	}
	opts := document.ListOptions{}
	if len(filters) > 0 {
		if m, ok := filters[0].(map[string]any); ok {
			opts.Filters = m
		}
	}
	docs, total, err := c.DocManager.GetList(c.docCtx(), doctype, opts)
	if err != nil {
		return nil, 0, err
	}
	result := make([]map[string]any, len(docs))
	for i, doc := range docs {
		result[i] = doc.AsMap()
	}
	return result, total, nil
}

// Insert creates a new document of the given doctype with the provided values.
// Returns the name (primary key) of the newly created document.
func (c *Console) Insert(doctype string, values map[string]any) (string, error) {
	if c.DocManager == nil {
		return "", fmt.Errorf("console: DocManager not initialized")
	}
	doc, err := c.DocManager.Insert(c.docCtx(), doctype, values)
	if err != nil {
		return "", err
	}
	return doc.Name(), nil
}

// Update modifies an existing document identified by doctype and name with the given values.
func (c *Console) Update(doctype, name string, values map[string]any) error {
	if c.DocManager == nil {
		return fmt.Errorf("console: DocManager not initialized")
	}
	_, err := c.DocManager.Update(c.docCtx(), doctype, name, values)
	return err
}

// Delete removes the document identified by doctype and name.
func (c *Console) Delete(doctype, name string) error {
	if c.DocManager == nil {
		return fmt.Errorf("console: DocManager not initialized")
	}
	return c.DocManager.Delete(c.docCtx(), doctype, name)
}

// SQL executes a raw SQL query against the site's database pool and returns
// the rows as a slice of maps keyed by column name.
func (c *Console) SQL(query string, args ...any) ([]map[string]any, error) {
	if c.Pool == nil {
		return nil, fmt.Errorf("console: database pool not initialized")
	}
	rows, err := c.Pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	descs := rows.FieldDescriptions()
	var result []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(descs))
		for i, desc := range descs {
			row[string(desc.Name)] = vals[i]
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// Meta returns the MetaType definition for the given doctype.
func (c *Console) Meta(doctype string) (*meta.MetaType, error) {
	if c.Registry == nil {
		return nil, fmt.Errorf("console: Registry not initialized")
	}
	return c.Registry.Get(context.Background(), c.Site.Name, doctype)
}
