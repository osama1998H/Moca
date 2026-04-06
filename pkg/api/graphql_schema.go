package api

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"

	"github.com/osama1998H/moca/pkg/meta"
)

// jsonScalar is a custom GraphQL scalar for arbitrary JSON values.
// Used for JSON, Geolocation, DynamicLink fields.
var jsonScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "JSON",
	Description: "Arbitrary JSON value",
	Serialize:   func(value interface{}) interface{} { return value },
	ParseValue:  func(value interface{}) interface{} { return value },
	ParseLiteral: func(valueAST ast.Value) interface{} {
		switch v := valueAST.(type) {
		case *ast.StringValue:
			return v.Value
		case *ast.IntValue:
			return v.Value
		case *ast.FloatValue:
			return v.Value
		case *ast.BooleanValue:
			return v.Value
		default:
			return nil
		}
	},
})

// fieldTypeToGraphQL maps a Moca FieldType to its GraphQL output type.
// Returns nil for types that should be excluded (Password, layout-only).
func fieldTypeToGraphQL(ft meta.FieldType) graphql.Output {
	switch ft {
	// String types
	case meta.FieldTypeData, meta.FieldTypeText, meta.FieldTypeLongText,
		meta.FieldTypeCode, meta.FieldTypeMarkdown, meta.FieldTypeHTMLEditor:
		return graphql.String

	// Integer
	case meta.FieldTypeInt:
		return graphql.Int

	// Number types
	case meta.FieldTypeFloat, meta.FieldTypeCurrency, meta.FieldTypePercent,
		meta.FieldTypeRating:
		return graphql.Float

	// Boolean
	case meta.FieldTypeCheck:
		return graphql.Boolean

	// Date/time types — stored as strings with specific formats.
	case meta.FieldTypeDate, meta.FieldTypeDatetime, meta.FieldTypeTime,
		meta.FieldTypeDuration:
		return graphql.String

	// Select is handled separately via buildEnumType, but falls back to String.
	case meta.FieldTypeSelect:
		return graphql.String

	// Reference types — the raw value is a string (document name).
	case meta.FieldTypeLink:
		return graphql.String
	case meta.FieldTypeDynamicLink:
		return jsonScalar

	// File types
	case meta.FieldTypeAttach, meta.FieldTypeAttachImage:
		return graphql.String

	// Structured types
	case meta.FieldTypeJSON, meta.FieldTypeGeolocation:
		return jsonScalar

	// Special string types
	case meta.FieldTypeColor, meta.FieldTypeSignature, meta.FieldTypeBarcode:
		return graphql.String

	// Password — never exposed in GraphQL.
	case meta.FieldTypePassword:
		return nil

	// Table types handled separately in buildObjectType.
	case meta.FieldTypeTable, meta.FieldTypeTableMultiSelect:
		return nil // sentinel — caller handles table fields

	default:
		return nil
	}
}

// sanitizeName converts a DocType or field name into a valid GraphQL name
// matching [_a-zA-Z][_a-zA-Z0-9]*.
func sanitizeName(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 1)
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-':
			b.WriteRune('_')
		default:
			b.WriteRune('_')
		}
	}
	name := b.String()
	if name == "" {
		return "_"
	}
	// Ensure first character is a letter or underscore.
	if name[0] >= '0' && name[0] <= '9' {
		name = "_" + name
	}
	return name
}

// toSnakeCase converts a PascalCase or camelCase string to snake_case for
// use in GraphQL query/mutation field names.
func toSnakeCase(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := rune(s[i-1])
				if unicode.IsLower(prev) || (i+1 < len(s) && unicode.IsLower(rune(s[i+1]))) {
					b.WriteRune('_')
				}
			}
			b.WriteRune(unicode.ToLower(r))
		} else if r == ' ' || r == '-' {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// buildEnumType creates a GraphQL Enum type from a Select field's newline-separated Options.
// Returns nil if no valid enum values are found.
func buildEnumType(doctypeName string, f *meta.FieldDef) *graphql.Enum {
	if f.Options == "" {
		return nil
	}
	opts := strings.Split(f.Options, "\n")
	values := graphql.EnumValueConfigMap{}
	for _, o := range opts {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		// Enum value names must match [_a-zA-Z][_a-zA-Z0-9]*.
		valueName := sanitizeName(o)
		values[valueName] = &graphql.EnumValueConfig{Value: o}
	}
	if len(values) == 0 {
		return nil
	}
	return graphql.NewEnum(graphql.EnumConfig{
		Name:   sanitizeName(doctypeName) + "_" + sanitizeName(f.Name) + "_enum",
		Values: values,
	})
}

// buildObjectType creates a GraphQL Object type from a MetaType.
// typeRegistry is used to resolve Link and Table field references to other DocTypes.
// resolverFactory is called for Link and Table fields to wire DataLoader resolution.
func buildObjectType(
	mt *meta.MetaType,
	typeRegistry map[string]*graphql.Object,
	resolverFactory *resolverFactory,
) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name:        sanitizeName(mt.Name),
		Description: mt.Description,
		Fields: graphql.FieldsThunk(func() graphql.Fields {
			fields := graphql.Fields{
				"name":        {Type: graphql.String, Description: "Document primary key"},
				"owner":       {Type: graphql.String, Description: "Creator user ID"},
				"creation":    {Type: graphql.String, Description: "Creation timestamp"},
				"modified":    {Type: graphql.String, Description: "Last modified timestamp"},
				"modified_by": {Type: graphql.String, Description: "Last modifier user ID"},
				"docstatus":   {Type: graphql.Int, Description: "Document status (0=Draft, 1=Submitted, 2=Cancelled)"},
			}

			var excludeFields, alwaysInclude map[string]bool
			if mt.APIConfig != nil {
				excludeFields = toSet(mt.APIConfig.ExcludeFields)
				alwaysInclude = toSet(mt.APIConfig.AlwaysInclude)
			}

			for i := range mt.Fields {
				f := &mt.Fields[i]

				// Skip non-storable (layout-only) types.
				if !f.FieldType.IsStorable() {
					continue
				}

				// Never expose password fields.
				if f.FieldType == meta.FieldTypePassword {
					continue
				}

				// Apply API inclusion rules.
				if !fieldIncluded(f, excludeFields, alwaysInclude) {
					continue
				}

				propName := f.Name
				if f.APIAlias != "" {
					propName = f.APIAlias
				}
				propName = sanitizeName(propName)

				// Select → enum type.
				if f.FieldType == meta.FieldTypeSelect {
					if enumType := buildEnumType(mt.Name, f); enumType != nil {
						fields[propName] = &graphql.Field{
							Type:        enumType,
							Description: f.Label,
						}
						continue
					}
					// Fallback to String if no valid options.
					fields[propName] = &graphql.Field{
						Type:        graphql.String,
						Description: f.Label,
					}
					continue
				}

				// Table / TableMultiSelect → list of child object type.
				if f.FieldType == meta.FieldTypeTable || f.FieldType == meta.FieldTypeTableMultiSelect {
					if f.Options != "" {
						if childType, ok := typeRegistry[f.Options]; ok {
							gf := &graphql.Field{
								Type:        graphql.NewList(childType),
								Description: f.Label,
							}
							if resolverFactory != nil {
								fieldName := f.Name
								gf.Resolve = makeTableFieldResolver(fieldName)
							}
							fields[propName] = gf
						}
					}
					continue
				}

				// Link → string field + resolved companion field.
				if f.FieldType == meta.FieldTypeLink && f.Options != "" {
					fields[propName] = &graphql.Field{
						Type:        graphql.String,
						Description: fmt.Sprintf("Link to %s (name)", f.Options),
					}
					// Add _data companion for full resolved object.
					if linkedType, ok := typeRegistry[f.Options]; ok {
						companionName := propName + "_data"
						if resolverFactory != nil {
							fields[companionName] = &graphql.Field{
								Type:        linkedType,
								Description: fmt.Sprintf("Resolved %s document", f.Options),
								Resolve:     makeLinkFieldResolver(resolverFactory.handler, f.Options, f.Name),
							}
						} else {
							fields[companionName] = &graphql.Field{
								Type:        linkedType,
								Description: fmt.Sprintf("Resolved %s document", f.Options),
							}
						}
					}
					continue
				}

				// All other field types.
				gqlType := fieldTypeToGraphQL(f.FieldType)
				if gqlType == nil {
					continue
				}
				fields[propName] = &graphql.Field{
					Type:        gqlType,
					Description: f.Label,
				}
			}

			return fields
		}),
	})
}

// buildInputType creates a GraphQL InputObject type for create/update mutations.
// Excludes read-only fields, system fields, and password fields.
func buildInputType(mt *meta.MetaType) *graphql.InputObject {
	fields := graphql.InputObjectConfigFieldMap{}

	for i := range mt.Fields {
		f := &mt.Fields[i]

		if !f.FieldType.IsStorable() {
			continue
		}
		if f.FieldType == meta.FieldTypePassword {
			continue
		}
		if f.ReadOnly || f.APIReadOnly {
			continue
		}

		propName := f.Name
		if f.APIAlias != "" {
			propName = f.APIAlias
		}
		propName = sanitizeName(propName)

		// Table fields accept JSON arrays in input.
		if f.FieldType == meta.FieldTypeTable || f.FieldType == meta.FieldTypeTableMultiSelect {
			fields[propName] = &graphql.InputObjectFieldConfig{
				Type:        graphql.NewList(jsonScalar),
				Description: f.Label,
			}
			continue
		}

		// Select — use String in input (enum values as strings).
		if f.FieldType == meta.FieldTypeSelect {
			fields[propName] = &graphql.InputObjectFieldConfig{
				Type:        graphql.String,
				Description: f.Label,
			}
			continue
		}

		gqlType := fieldTypeToGraphQL(f.FieldType)
		if gqlType == nil {
			continue
		}

		// Convert Output to Input type.
		var inputType graphql.Input
		switch gqlType {
		case graphql.String:
			inputType = graphql.String
		case graphql.Int:
			inputType = graphql.Int
		case graphql.Float:
			inputType = graphql.Float
		case graphql.Boolean:
			inputType = graphql.Boolean
		default:
			inputType = jsonScalar
		}

		fields[propName] = &graphql.InputObjectFieldConfig{
			Type:        inputType,
			Description: f.Label,
		}
	}

	return graphql.NewInputObject(graphql.InputObjectConfig{
		Name:   sanitizeName(mt.Name) + "_input",
		Fields: fields,
	})
}

// buildSchema assembles a complete GraphQL schema from a list of MetaTypes.
// Only API-enabled, non-child-table MetaTypes get top-level query/mutation fields.
func buildSchema(metatypes []*meta.MetaType, rf *resolverFactory) (*graphql.Schema, error) {
	// Phase 1: Build object types for all MetaTypes (including child tables).
	typeRegistry := make(map[string]*graphql.Object, len(metatypes))
	apiEnabled := make([]*meta.MetaType, 0, len(metatypes))

	for _, mt := range metatypes {
		typeRegistry[mt.Name] = buildObjectType(mt, typeRegistry, rf)
		if mt.APIConfig != nil && mt.APIConfig.Enabled {
			apiEnabled = append(apiEnabled, mt)
		}
	}

	// Phase 2: Build query and mutation fields.
	queryFields := graphql.Fields{}
	mutationFields := graphql.Fields{}

	for _, mt := range apiEnabled {
		// Child tables don't get top-level operations.
		if mt.IsChildTable {
			continue
		}

		snakeName := toSnakeCase(mt.Name)
		objType := typeRegistry[mt.Name]

		// Query: get single document.
		if mt.APIConfig.AllowGet {
			queryFields[snakeName] = &graphql.Field{
				Type:        objType,
				Description: fmt.Sprintf("Get a single %s by name", mt.Label),
				Args: graphql.FieldConfigArgument{
					"name": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(graphql.String),
						Description: "Document primary key",
					},
				},
			}
			if rf != nil {
				queryFields[snakeName].Resolve = makeGetResolver(rf.handler, mt.Name)
			}
		}

		// Query: list documents.
		if mt.APIConfig.AllowList {
			listName := "all_" + snakeName
			queryFields[listName] = &graphql.Field{
				Type:        graphql.NewList(objType),
				Description: fmt.Sprintf("List %s documents", mt.Label),
				Args: graphql.FieldConfigArgument{
					"limit": &graphql.ArgumentConfig{
						Type:         graphql.Int,
						DefaultValue: 20,
						Description:  "Maximum number of documents to return",
					},
					"offset": &graphql.ArgumentConfig{
						Type:         graphql.Int,
						DefaultValue: 0,
						Description:  "Number of documents to skip",
					},
					"order_by": &graphql.ArgumentConfig{
						Type:        graphql.String,
						Description: "Field to sort by (e.g. 'modified desc')",
					},
					"filters": &graphql.ArgumentConfig{
						Type:        jsonScalar,
						Description: "Filter conditions as JSON",
					},
				},
			}
			if rf != nil {
				queryFields[listName].Resolve = makeListResolver(rf.handler, mt.Name)
			}
		}

		inputType := buildInputType(mt)

		// Mutation: create document.
		if mt.APIConfig.AllowCreate && !mt.IsSingle {
			createName := "create_" + snakeName
			mutationFields[createName] = &graphql.Field{
				Type:        objType,
				Description: fmt.Sprintf("Create a new %s document", mt.Label),
				Args: graphql.FieldConfigArgument{
					"input": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(inputType),
						Description: "Document fields",
					},
				},
			}
			if rf != nil {
				mutationFields[createName].Resolve = makeCreateResolver(rf.handler, mt.Name)
			}
		}

		// Mutation: update document.
		if mt.APIConfig.AllowUpdate {
			updateName := "update_" + snakeName
			mutationFields[updateName] = &graphql.Field{
				Type:        objType,
				Description: fmt.Sprintf("Update an existing %s document", mt.Label),
				Args: graphql.FieldConfigArgument{
					"name": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(graphql.String),
						Description: "Document primary key",
					},
					"input": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(inputType),
						Description: "Fields to update",
					},
				},
			}
			if rf != nil {
				mutationFields[updateName].Resolve = makeUpdateResolver(rf.handler, mt.Name)
			}
		}

		// Mutation: delete document.
		if mt.APIConfig.AllowDelete && !mt.IsSingle {
			deleteName := "delete_" + snakeName
			mutationFields[deleteName] = &graphql.Field{
				Type:        graphql.Boolean,
				Description: fmt.Sprintf("Delete a %s document", mt.Label),
				Args: graphql.FieldConfigArgument{
					"name": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(graphql.String),
						Description: "Document primary key",
					},
				},
			}
			if rf != nil {
				mutationFields[deleteName].Resolve = makeDeleteResolver(rf.handler, mt.Name)
			}
		}
	}

	// Ensure at least one query field exists (GraphQL requires a non-empty Query type).
	if len(queryFields) == 0 {
		queryFields["_empty"] = &graphql.Field{
			Type:        graphql.String,
			Description: "Placeholder — no API-enabled DocTypes registered",
			Resolve:     func(p graphql.ResolveParams) (interface{}, error) { return nil, nil },
		}
	}

	schemaConfig := graphql.SchemaConfig{
		Query: graphql.NewObject(graphql.ObjectConfig{
			Name:   "Query",
			Fields: queryFields,
		}),
	}

	if len(mutationFields) > 0 {
		schemaConfig.Mutation = graphql.NewObject(graphql.ObjectConfig{
			Name:   "Mutation",
			Fields: mutationFields,
		})
	}

	schema, err := graphql.NewSchema(schemaConfig)
	if err != nil {
		return nil, fmt.Errorf("build graphql schema: %w", err)
	}
	return &schema, nil
}

