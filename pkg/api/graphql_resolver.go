package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/graph-gophers/dataloader/v7"
	"github.com/graphql-go/graphql"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/orm"
)

// dataloaderKey is the context key for the per-request DataLoader map.
type dataloaderKey struct{}

// docLoaderMap holds per-DocType DataLoaders for a single GraphQL request.
type docLoaderMap struct {
	loaders map[string]*dataloader.Loader[string, map[string]any]
	mu      sync.Mutex
}

// resolverFactory holds the handler reference needed to create resolvers.
type resolverFactory struct {
	handler *GraphQLHandler
}

// withDataLoaders attaches a fresh DataLoader map to the context.
func withDataLoaders(ctx context.Context) context.Context {
	return context.WithValue(ctx, dataloaderKey{}, &docLoaderMap{
		loaders: make(map[string]*dataloader.Loader[string, map[string]any]),
	})
}

// getDocLoader returns (or creates) a DataLoader for the given doctype+site.
// The loader batches multiple Get-by-name calls into a single GetList with IN filter.
func getDocLoader(ctx context.Context, h *GraphQLHandler, site, doctype string) *dataloader.Loader[string, map[string]any] {
	dlm, _ := ctx.Value(dataloaderKey{}).(*docLoaderMap)
	if dlm == nil {
		dlm = &docLoaderMap{loaders: make(map[string]*dataloader.Loader[string, map[string]any])}
	}
	key := site + ":" + doctype

	dlm.mu.Lock()
	defer dlm.mu.Unlock()

	if loader, ok := dlm.loaders[key]; ok {
		return loader
	}

	batchFn := func(ctx context.Context, keys []string) []*dataloader.Result[map[string]any] {
		results := make([]*dataloader.Result[map[string]any], len(keys))

		siteCtx := SiteFromContext(ctx)
		user := UserFromContext(ctx)
		if siteCtx == nil || user == nil {
			for i := range results {
				results[i] = &dataloader.Result[map[string]any]{Error: fmt.Errorf("missing site or user context")}
			}
			return results
		}

		docCtx := newDocContext(ctx, siteCtx, user)
		opts := document.ListOptions{
			AdvancedFilters: []orm.Filter{
				{Field: "name", Operator: orm.OpIn, Value: keys},
			},
			Limit: len(keys),
		}

		docs, _, err := h.crud.GetList(docCtx, doctype, opts)
		if err != nil {
			for i := range results {
				results[i] = &dataloader.Result[map[string]any]{Error: err}
			}
			return results
		}

		// Index results by name for ordered return.
		byName := make(map[string]map[string]any, len(docs))
		for _, doc := range docs {
			m := doc.AsMap()
			if name, ok := m["name"].(string); ok {
				byName[name] = m
			}
		}

		for i, key := range keys {
			if m, ok := byName[key]; ok {
				results[i] = &dataloader.Result[map[string]any]{Data: m}
			} else {
				results[i] = &dataloader.Result[map[string]any]{Data: nil}
			}
		}
		return results
	}

	loader := dataloader.NewBatchedLoader(batchFn)
	dlm.loaders[key] = loader
	return loader
}

// makeGetResolver creates a resolver for fetching a single document by name.
func makeGetResolver(h *GraphQLHandler, doctype string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		site := SiteFromContext(p.Context)
		user := UserFromContext(p.Context)
		if site == nil || user == nil {
			return nil, fmt.Errorf("missing site or user context")
		}

		if err := h.perm.CheckDocPerm(p.Context, user, doctype, "read"); err != nil {
			return nil, toGraphQLError(err)
		}

		name, _ := p.Args["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name argument is required")
		}

		docCtx := newDocContext(p.Context, site, user)
		doc, err := h.crud.Get(docCtx, doctype, name)
		if err != nil {
			return nil, toGraphQLError(err)
		}

		result := doc.AsMap()
		if h.fieldPerm != nil {
			mt, merr := h.meta.Get(p.Context, site.Name, doctype)
			if merr == nil {
				ctx := WithOperationType(p.Context, OpGet)
				transformed, terr := h.fieldPerm.TransformResponse(ctx, mt, result)
				if terr == nil {
					result = transformed
				}
			}
		}

		return result, nil
	}
}

// makeListResolver creates a resolver for listing documents with pagination and filters.
func makeListResolver(h *GraphQLHandler, doctype string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		site := SiteFromContext(p.Context)
		user := UserFromContext(p.Context)
		if site == nil || user == nil {
			return nil, fmt.Errorf("missing site or user context")
		}

		if err := h.perm.CheckDocPerm(p.Context, user, doctype, "read"); err != nil {
			return nil, toGraphQLError(err)
		}

		limit, _ := p.Args["limit"].(int)
		offset, _ := p.Args["offset"].(int)
		if limit <= 0 {
			limit = 20
		}
		if limit > 100 {
			limit = 100
		}

		opts := document.ListOptions{
			Limit:  limit,
			Offset: offset,
		}

		if orderBy, ok := p.Args["order_by"].(string); ok && orderBy != "" {
			parts := strings.Fields(orderBy)
			if len(parts) >= 1 {
				opts.OrderBy = parts[0]
			}
			if len(parts) >= 2 {
				dir := strings.ToUpper(parts[1])
				if dir == "ASC" || dir == "DESC" {
					opts.OrderDir = dir
				}
			}
		}

		if filters, ok := p.Args["filters"].(map[string]interface{}); ok {
			opts.Filters = make(map[string]any, len(filters))
			for k, v := range filters {
				opts.Filters[k] = v
			}
		}

		docCtx := newDocContext(p.Context, site, user)
		docs, _, err := h.crud.GetList(docCtx, doctype, opts)
		if err != nil {
			return nil, toGraphQLError(err)
		}

		results := make([]map[string]any, len(docs))
		for i, doc := range docs {
			m := doc.AsMap()
			if h.fieldPerm != nil {
				mt, merr := h.meta.Get(p.Context, site.Name, doctype)
				if merr == nil {
					ctx := WithOperationType(p.Context, OpList)
					if transformed, terr := h.fieldPerm.TransformResponse(ctx, mt, m); terr == nil {
						m = transformed
					}
				}
			}
			results[i] = m
		}

		return results, nil
	}
}

// makeCreateResolver creates a resolver for inserting a new document.
func makeCreateResolver(h *GraphQLHandler, doctype string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		site := SiteFromContext(p.Context)
		user := UserFromContext(p.Context)
		if site == nil || user == nil {
			return nil, fmt.Errorf("missing site or user context")
		}

		if err := h.perm.CheckDocPerm(p.Context, user, doctype, "create"); err != nil {
			return nil, toGraphQLError(err)
		}

		input, ok := p.Args["input"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("input argument is required")
		}

		docCtx := newDocContext(p.Context, site, user)
		doc, err := h.crud.Insert(docCtx, doctype, input)
		if err != nil {
			return nil, toGraphQLError(err)
		}

		return doc.AsMap(), nil
	}
}

// makeUpdateResolver creates a resolver for updating an existing document.
func makeUpdateResolver(h *GraphQLHandler, doctype string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		site := SiteFromContext(p.Context)
		user := UserFromContext(p.Context)
		if site == nil || user == nil {
			return nil, fmt.Errorf("missing site or user context")
		}

		if err := h.perm.CheckDocPerm(p.Context, user, doctype, "write"); err != nil {
			return nil, toGraphQLError(err)
		}

		name, _ := p.Args["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name argument is required")
		}

		input, ok := p.Args["input"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("input argument is required")
		}

		docCtx := newDocContext(p.Context, site, user)
		doc, err := h.crud.Update(docCtx, doctype, name, input)
		if err != nil {
			return nil, toGraphQLError(err)
		}

		return doc.AsMap(), nil
	}
}

// makeDeleteResolver creates a resolver for deleting a document.
func makeDeleteResolver(h *GraphQLHandler, doctype string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		site := SiteFromContext(p.Context)
		user := UserFromContext(p.Context)
		if site == nil || user == nil {
			return nil, fmt.Errorf("missing site or user context")
		}

		if err := h.perm.CheckDocPerm(p.Context, user, doctype, "delete"); err != nil {
			return nil, toGraphQLError(err)
		}

		name, _ := p.Args["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name argument is required")
		}

		docCtx := newDocContext(p.Context, site, user)
		if err := h.crud.Delete(docCtx, doctype, name); err != nil {
			return nil, toGraphQLError(err)
		}

		return true, nil
	}
}

// makeLinkFieldResolver creates a resolver for a Link field's companion _data field.
// Uses DataLoader to batch multiple link resolutions into a single query.
func makeLinkFieldResolver(h *GraphQLHandler, linkedDoctype, fieldName string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		source, ok := p.Source.(map[string]any)
		if !ok {
			return nil, nil
		}

		nameVal, ok := source[fieldName]
		if !ok || nameVal == nil {
			return nil, nil
		}

		name, ok := nameVal.(string)
		if !ok || name == "" {
			return nil, nil
		}

		site := SiteFromContext(p.Context)
		if site == nil {
			return nil, nil
		}

		// Permission check for linked doctype.
		user := UserFromContext(p.Context)
		if user == nil {
			return nil, nil
		}
		if err := h.perm.CheckDocPerm(p.Context, user, linkedDoctype, "read"); err != nil {
			return nil, nil // silently omit — caller may not have permission on linked doctype
		}

		loader := getDocLoader(p.Context, h, site.Name, linkedDoctype)
		thunk := loader.Load(p.Context, name)
		result, err := thunk()
		if err != nil {
			return nil, nil // gracefully handle missing links
		}
		return result, nil
	}
}

// makeTableFieldResolver creates a resolver for Table/TableMultiSelect fields.
// Child rows are already included in the parent document's AsMap output.
func makeTableFieldResolver(fieldName string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		source, ok := p.Source.(map[string]any)
		if !ok {
			return nil, nil
		}
		return source[fieldName], nil
	}
}

// toGraphQLError converts domain errors to descriptive messages.
// GraphQL always returns HTTP 200; errors are conveyed in the response body.
func toGraphQLError(err error) error {
	if err == nil {
		return nil
	}

	var permErr *PermissionDeniedError
	if errors.As(err, &permErr) {
		return fmt.Errorf("permission denied: %s", permErr.Error())
	}

	var notFound *document.DocNotFoundError
	if errors.As(err, &notFound) {
		return fmt.Errorf("not found: %s", notFound.Error())
	}

	var valErr *document.ValidationError
	if errors.As(err, &valErr) {
		return fmt.Errorf("validation error: %s", valErr.Error())
	}

	return err
}
