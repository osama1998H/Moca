package api

import (
	"fmt"
	"sort"
	"strings"

	"github.com/osama1998H/moca/pkg/meta"
)

// ---------------------------------------------------------------------------
// OpenAPI 3.0.3 struct definitions
// ---------------------------------------------------------------------------

// OpenAPISpec is a minimal OpenAPI 3.0.3 document.
type OpenAPISpec struct {
	OpenAPI    string                `json:"openapi" yaml:"openapi"`
	Info       OpenAPIInfo           `json:"info" yaml:"info"`
	Servers    []OpenAPIServer       `json:"servers,omitempty" yaml:"servers,omitempty"`
	Paths      map[string]*PathItem  `json:"paths" yaml:"paths"`
	Components *Components           `json:"components,omitempty" yaml:"components,omitempty"`
	Security   []map[string][]string `json:"security,omitempty" yaml:"security,omitempty"`
}

// OpenAPIInfo holds API metadata.
type OpenAPIInfo struct {
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Version     string `json:"version" yaml:"version"`
}

// OpenAPIServer describes a base URL.
type OpenAPIServer struct {
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Components holds reusable schemas and security schemes.
type Components struct {
	Schemas         map[string]*Schema         `json:"schemas,omitempty" yaml:"schemas,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
}

// SecurityScheme defines an authentication mechanism.
type SecurityScheme struct {
	Type         string `json:"type" yaml:"type"`
	Scheme       string `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty" yaml:"bearerFormat,omitempty"`
	In           string `json:"in,omitempty" yaml:"in,omitempty"`
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`
	Description  string `json:"description,omitempty" yaml:"description,omitempty"`
}

// PathItem groups operations by HTTP method on a single path.
type PathItem struct {
	Get    *Operation `json:"get,omitempty" yaml:"get,omitempty"`
	Post   *Operation `json:"post,omitempty" yaml:"post,omitempty"`
	Put    *Operation `json:"put,omitempty" yaml:"put,omitempty"`
	Delete *Operation `json:"delete,omitempty" yaml:"delete,omitempty"`
	Patch  *Operation `json:"patch,omitempty" yaml:"patch,omitempty"`
}

// Operation describes a single API operation.
type Operation struct {
	Responses   map[string]Response `json:"responses" yaml:"responses"`
	RequestBody *RequestBody        `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Summary     string              `json:"summary,omitempty" yaml:"summary,omitempty"`
	OperationID string              `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Tags        []string            `json:"tags,omitempty" yaml:"tags,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// Parameter describes a single operation parameter.
type Parameter struct {
	Schema      *Schema `json:"schema,omitempty" yaml:"schema,omitempty"`
	Name        string  `json:"name" yaml:"name"`
	In          string  `json:"in" yaml:"in"` // query, path, header
	Description string  `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool    `json:"required,omitempty" yaml:"required,omitempty"`
}

// RequestBody describes the request payload.
type RequestBody struct {
	Content     map[string]MediaType `json:"content" yaml:"content"`
	Description string               `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool                 `json:"required,omitempty" yaml:"required,omitempty"`
}

// MediaType describes a media type with a schema.
type MediaType struct {
	Schema *Schema `json:"schema,omitempty" yaml:"schema,omitempty"`
}

// Response describes a single response from an API operation.
type Response struct {
	Content     map[string]MediaType `json:"content,omitempty" yaml:"content,omitempty"`
	Description string               `json:"description" yaml:"description"`
}

// Schema describes a data model (JSON Schema subset for OpenAPI 3.0).
type Schema struct {
	Properties  map[string]*Schema `json:"properties,omitempty" yaml:"properties,omitempty"`
	Items       *Schema            `json:"items,omitempty" yaml:"items,omitempty"`
	MaxLength   *int               `json:"maxLength,omitempty" yaml:"maxLength,omitempty"`
	Minimum     *float64           `json:"minimum,omitempty" yaml:"minimum,omitempty"`
	Maximum     *float64           `json:"maximum,omitempty" yaml:"maximum,omitempty"`
	Ref         string             `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Type        string             `json:"type,omitempty" yaml:"type,omitempty"`
	Format      string             `json:"format,omitempty" yaml:"format,omitempty"`
	Description string             `json:"description,omitempty" yaml:"description,omitempty"`
	Required    []string           `json:"required,omitempty" yaml:"required,omitempty"`
	Enum        []string           `json:"enum,omitempty" yaml:"enum,omitempty"`
	ReadOnly    bool               `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
}

// ---------------------------------------------------------------------------
// FieldType to OpenAPI type mapping
// ---------------------------------------------------------------------------

// FieldTypeToOpenAPI maps a Moca FieldType to its OpenAPI (type, format) pair.
// Layout-only types return empty strings.
func FieldTypeToOpenAPI(ft meta.FieldType) (oaType, oaFormat string) {
	switch ft {
	// String types
	case meta.FieldTypeData, meta.FieldTypeText, meta.FieldTypeLongText,
		meta.FieldTypeCode, meta.FieldTypeMarkdown, meta.FieldTypeHTMLEditor:
		return "string", ""

	// Integer
	case meta.FieldTypeInt:
		return "integer", "int64"

	// Number types
	case meta.FieldTypeFloat, meta.FieldTypeCurrency, meta.FieldTypePercent:
		return "number", "double"

	// Boolean
	case meta.FieldTypeCheck:
		return "boolean", ""

	// Date/time types
	case meta.FieldTypeDate:
		return "string", "date"
	case meta.FieldTypeDatetime:
		return "string", "date-time"
	case meta.FieldTypeTime:
		return "string", "time"
	case meta.FieldTypeDuration:
		return "string", "duration"

	// Select (enum handled separately via Options)
	case meta.FieldTypeSelect:
		return "string", ""

	// Reference types
	case meta.FieldTypeLink, meta.FieldTypeDynamicLink:
		return "string", ""

	// File types
	case meta.FieldTypeAttach, meta.FieldTypeAttachImage:
		return "string", "uri"

	// Table types (array — items set by caller)
	case meta.FieldTypeTable, meta.FieldTypeTableMultiSelect:
		return "array", ""

	// Structured types
	case meta.FieldTypeJSON, meta.FieldTypeGeolocation:
		return "object", ""

	// Special string types
	case meta.FieldTypePassword:
		return "string", "password"
	case meta.FieldTypeColor:
		return "string", ""
	case meta.FieldTypeRating:
		return "number", ""
	case meta.FieldTypeSignature:
		return "string", ""
	case meta.FieldTypeBarcode:
		return "string", ""

	default:
		return "", ""
	}
}

// ---------------------------------------------------------------------------
// Schema generation
// ---------------------------------------------------------------------------

// fieldIncluded reports whether a field should appear in the OpenAPI schema.
func fieldIncluded(f *meta.FieldDef, excludeFields map[string]bool, alwaysInclude map[string]bool) bool {
	// Layout-only types never appear in API.
	if !f.FieldType.IsStorable() {
		return false
	}
	// Always include wins over everything.
	if alwaysInclude[f.Name] {
		return true
	}
	// Explicit exclude.
	if excludeFields[f.Name] {
		return false
	}
	// InAPI flag (defaults to true for storable fields when not explicitly set
	// in JSON — the zero value false means "not in API" only if the field was
	// explicitly excluded, but for backwards compat we include all storable
	// fields unless excluded).
	return true
}

// SchemaFromMetaType generates an OpenAPI Schema from a MetaType's fields.
// excludeFields and alwaysInclude are taken from APIConfig.
func SchemaFromMetaType(mt *meta.MetaType, excludeFields, alwaysInclude []string) *Schema {
	excl := toSet(excludeFields)
	incl := toSet(alwaysInclude)

	props := make(map[string]*Schema)
	var required []string

	// Standard fields present on every document.
	props["name"] = &Schema{Type: "string", Description: "Document primary key"}
	props["owner"] = &Schema{Type: "string", Description: "Creator user ID"}
	props["creation"] = &Schema{Type: "string", Format: "date-time", Description: "Creation timestamp"}
	props["modified"] = &Schema{Type: "string", Format: "date-time", Description: "Last modified timestamp"}
	props["modified_by"] = &Schema{Type: "string", Description: "Last modifier user ID"}

	for i := range mt.Fields {
		f := &mt.Fields[i]
		if !fieldIncluded(f, excl, incl) {
			continue
		}

		propName := f.Name
		if f.APIAlias != "" {
			propName = f.APIAlias
		}

		oaType, oaFormat := FieldTypeToOpenAPI(f.FieldType)
		if oaType == "" {
			continue
		}

		s := &Schema{
			Type:   oaType,
			Format: oaFormat,
		}

		if f.Label != "" {
			s.Description = f.Label
		}

		// Select → enum from Options (newline-separated).
		if f.FieldType == meta.FieldTypeSelect && f.Options != "" {
			opts := strings.Split(f.Options, "\n")
			var enums []string
			for _, o := range opts {
				o = strings.TrimSpace(o)
				if o != "" {
					enums = append(enums, o)
				}
			}
			if len(enums) > 0 {
				s.Enum = enums
			}
		}

		// Link → description references linked DocType.
		if f.FieldType == meta.FieldTypeLink && f.Options != "" {
			s.Description = fmt.Sprintf("Link to %s", f.Options)
		}

		// Table → array with $ref to child DocType schema.
		if f.FieldType == meta.FieldTypeTable || f.FieldType == meta.FieldTypeTableMultiSelect {
			if f.Options != "" {
				s.Items = &Schema{Ref: "#/components/schemas/" + f.Options}
			} else {
				s.Items = &Schema{Type: "object"}
			}
		}

		// Validation constraints.
		if f.MaxLength > 0 {
			ml := f.MaxLength
			s.MaxLength = &ml
		}
		if f.MinValue != nil {
			s.Minimum = f.MinValue
		}
		if f.MaxValue != nil {
			s.Maximum = f.MaxValue
		}

		if f.APIReadOnly || f.ReadOnly {
			s.ReadOnly = true
		}

		if f.Required {
			required = append(required, propName)
		}

		props[propName] = s
	}

	// ComputedFields are read-only additions.
	if mt.APIConfig != nil {
		for _, cf := range mt.APIConfig.ComputedFields {
			s := &Schema{ReadOnly: true}
			switch cf.Type {
			case "string":
				s.Type = "string"
			case "number", "float":
				s.Type = "number"
			case "integer", "int":
				s.Type = "integer"
			case "boolean", "bool":
				s.Type = "boolean"
			default:
				s.Type = "string"
			}
			props[cf.Name] = s
		}
	}

	schema := &Schema{
		Type:       "object",
		Properties: props,
	}
	if len(required) > 0 {
		sort.Strings(required)
		schema.Required = required
	}
	return schema
}

// ---------------------------------------------------------------------------
// Spec generation
// ---------------------------------------------------------------------------

// SpecOptions controls OpenAPI spec generation.
type SpecOptions struct {
	Title       string
	Description string
	Version     string
	ServerURL   string
}

// GenerateSpec produces a complete OpenAPI 3.0.3 specification from a set of
// MetaTypes and whitelisted method names.
func GenerateSpec(metatypes []meta.MetaType, methods []string, opts SpecOptions) *OpenAPISpec {
	if opts.Title == "" {
		opts.Title = "Moca API"
	}
	if opts.Version == "" {
		opts.Version = "1.0.0"
	}

	spec := &OpenAPISpec{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       opts.Title,
			Description: opts.Description,
			Version:     opts.Version,
		},
		Paths: make(map[string]*PathItem),
		Components: &Components{
			Schemas:         make(map[string]*Schema),
			SecuritySchemes: securitySchemes(),
		},
		Security: []map[string][]string{
			{"bearerAuth": {}},
			{"apiKeyAuth": {}},
			{"sessionAuth": {}},
		},
	}

	if opts.ServerURL != "" {
		spec.Servers = []OpenAPIServer{{URL: opts.ServerURL}}
	}

	// Sort MetaTypes for deterministic output.
	sorted := make([]meta.MetaType, len(metatypes))
	copy(sorted, metatypes)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	for i := range sorted {
		mt := &sorted[i]
		cfg := mt.APIConfig
		if cfg == nil || !cfg.Enabled {
			continue
		}

		// Generate component schema.
		spec.Components.Schemas[mt.Name] = SchemaFromMetaType(mt, cfg.ExcludeFields, cfg.AlwaysInclude)

		tag := mt.Name

		// Meta endpoint — always available for enabled types.
		addMetaPath(spec, mt.Name, tag)

		// CRUD endpoints based on Allow* flags.
		if cfg.AllowList {
			addListPath(spec, mt.Name, tag)
		}
		if cfg.AllowGet {
			addGetPath(spec, mt.Name, tag)
		}
		if cfg.AllowCreate && !mt.IsSingle {
			addCreatePath(spec, mt.Name, tag)
		}
		if cfg.AllowUpdate {
			addUpdatePath(spec, mt.Name, tag)
		}
		if cfg.AllowDelete && !mt.IsSingle {
			addDeletePath(spec, mt.Name, tag)
		}

		// Custom endpoints.
		for _, ce := range cfg.CustomEndpoints {
			addCustomEndpointPath(spec, mt.Name, tag, &ce)
		}
	}

	// Whitelisted methods.
	sort.Strings(methods)
	for _, name := range methods {
		addMethodPath(spec, name)
	}

	// Add standard error schema.
	spec.Components.Schemas["ErrorResponse"] = &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"error": {
				Type: "object",
				Properties: map[string]*Schema{
					"code":    {Type: "string"},
					"message": {Type: "string"},
				},
			},
		},
	}

	return spec
}

// ---------------------------------------------------------------------------
// Path builders
// ---------------------------------------------------------------------------

func securitySchemes() map[string]*SecurityScheme {
	return map[string]*SecurityScheme{
		"bearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "JWT Bearer token",
		},
		"apiKeyAuth": {
			Type:        "apiKey",
			In:          "header",
			Name:        "Authorization",
			Description: "API key authentication: token KEY_ID:SECRET",
		},
		"sessionAuth": {
			Type:        "apiKey",
			In:          "cookie",
			Name:        "sid",
			Description: "Session cookie",
		},
	}
}

var jsonContent = map[string]MediaType{
	"application/json": {},
}

func errorResponses() map[string]Response {
	return map[string]Response{
		"401": {Description: "Authentication required"},
		"403": {Description: "Permission denied"},
		"500": {Description: "Internal server error"},
	}
}

func ensurePathItem(spec *OpenAPISpec, path string) *PathItem {
	pi, ok := spec.Paths[path]
	if !ok {
		pi = &PathItem{}
		spec.Paths[path] = pi
	}
	return pi
}

func schemaRef(doctype string) *Schema {
	return &Schema{Ref: "#/components/schemas/" + doctype}
}

func dataWrapper(inner *Schema) *Schema {
	return &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"data": inner,
		},
	}
}

func listWrapper(doctype string) *Schema {
	return &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"data": {
				Type:  "array",
				Items: schemaRef(doctype),
			},
			"total": {Type: "integer"},
		},
	}
}

func nameParam() Parameter {
	return Parameter{
		Name:     "name",
		In:       "path",
		Required: true,
		Schema:   &Schema{Type: "string"},
	}
}

func listParams() []Parameter {
	return []Parameter{
		{Name: "limit", In: "query", Schema: &Schema{Type: "integer"}, Description: "Page size"},
		{Name: "offset", In: "query", Schema: &Schema{Type: "integer"}, Description: "Offset for pagination"},
		{Name: "order_by", In: "query", Schema: &Schema{Type: "string"}, Description: "Sort field and direction (e.g. 'creation desc')"},
		{Name: "filters", In: "query", Schema: &Schema{Type: "string"}, Description: "JSON-encoded filter conditions"},
		{Name: "fields", In: "query", Schema: &Schema{Type: "string"}, Description: "Comma-separated list of fields to include"},
	}
}

func addMetaPath(spec *OpenAPISpec, doctype, tag string) {
	path := fmt.Sprintf("/api/v1/meta/%s", doctype)
	pi := ensurePathItem(spec, path)
	resps := errorResponses()
	resps["200"] = Response{
		Description: "MetaType definition",
		Content:     jsonContent,
	}
	pi.Get = &Operation{
		Tags:        []string{tag},
		Summary:     fmt.Sprintf("Get %s metadata", doctype),
		OperationID: fmt.Sprintf("getMeta%s", doctype),
		Responses:   resps,
	}
}

func addListPath(spec *OpenAPISpec, doctype, tag string) {
	path := fmt.Sprintf("/api/v1/resource/%s", doctype)
	pi := ensurePathItem(spec, path)
	resps := errorResponses()
	resps["200"] = Response{
		Description: fmt.Sprintf("List of %s documents", doctype),
		Content: map[string]MediaType{
			"application/json": {Schema: listWrapper(doctype)},
		},
	}
	pi.Get = &Operation{
		Tags:        []string{tag},
		Summary:     fmt.Sprintf("List %s", doctype),
		OperationID: fmt.Sprintf("list%s", doctype),
		Parameters:  listParams(),
		Responses:   resps,
	}
}

func addGetPath(spec *OpenAPISpec, doctype, tag string) {
	path := fmt.Sprintf("/api/v1/resource/%s/{name}", doctype)
	pi := ensurePathItem(spec, path)
	resps := errorResponses()
	resps["200"] = Response{
		Description: fmt.Sprintf("%s document", doctype),
		Content: map[string]MediaType{
			"application/json": {Schema: dataWrapper(schemaRef(doctype))},
		},
	}
	resps["404"] = Response{Description: "Document not found"}
	pi.Get = &Operation{
		Tags:        []string{tag},
		Summary:     fmt.Sprintf("Get %s by name", doctype),
		OperationID: fmt.Sprintf("get%s", doctype),
		Parameters:  []Parameter{nameParam()},
		Responses:   resps,
	}
}

func addCreatePath(spec *OpenAPISpec, doctype, tag string) {
	path := fmt.Sprintf("/api/v1/resource/%s", doctype)
	pi := ensurePathItem(spec, path)
	resps := errorResponses()
	resps["201"] = Response{
		Description: fmt.Sprintf("Created %s document", doctype),
		Content: map[string]MediaType{
			"application/json": {Schema: dataWrapper(schemaRef(doctype))},
		},
	}
	resps["400"] = Response{Description: "Validation error"}
	pi.Post = &Operation{
		Tags:        []string{tag},
		Summary:     fmt.Sprintf("Create %s", doctype),
		OperationID: fmt.Sprintf("create%s", doctype),
		RequestBody: &RequestBody{
			Required: true,
			Content: map[string]MediaType{
				"application/json": {Schema: schemaRef(doctype)},
			},
		},
		Responses: resps,
	}
}

func addUpdatePath(spec *OpenAPISpec, doctype, tag string) {
	path := fmt.Sprintf("/api/v1/resource/%s/{name}", doctype)
	pi := ensurePathItem(spec, path)
	resps := errorResponses()
	resps["200"] = Response{
		Description: fmt.Sprintf("Updated %s document", doctype),
		Content: map[string]MediaType{
			"application/json": {Schema: dataWrapper(schemaRef(doctype))},
		},
	}
	resps["400"] = Response{Description: "Validation error"}
	resps["404"] = Response{Description: "Document not found"}
	pi.Put = &Operation{
		Tags:        []string{tag},
		Summary:     fmt.Sprintf("Update %s", doctype),
		OperationID: fmt.Sprintf("update%s", doctype),
		Parameters:  []Parameter{nameParam()},
		RequestBody: &RequestBody{
			Required: true,
			Content: map[string]MediaType{
				"application/json": {Schema: schemaRef(doctype)},
			},
		},
		Responses: resps,
	}
}

func addDeletePath(spec *OpenAPISpec, doctype, tag string) {
	path := fmt.Sprintf("/api/v1/resource/%s/{name}", doctype)
	pi := ensurePathItem(spec, path)
	resps := errorResponses()
	resps["200"] = Response{Description: fmt.Sprintf("Deleted %s", doctype)}
	resps["404"] = Response{Description: "Document not found"}
	pi.Delete = &Operation{
		Tags:        []string{tag},
		Summary:     fmt.Sprintf("Delete %s", doctype),
		OperationID: fmt.Sprintf("delete%s", doctype),
		Parameters:  []Parameter{nameParam()},
		Responses:   resps,
	}
}

func addCustomEndpointPath(spec *OpenAPISpec, doctype, tag string, ce *meta.CustomEndpoint) {
	path := fmt.Sprintf("/api/v1/custom/%s/%s", doctype, strings.TrimPrefix(ce.Path, "/"))
	pi := ensurePathItem(spec, path)

	resps := errorResponses()
	resps["200"] = Response{
		Description: "Success",
		Content:     jsonContent,
	}

	op := &Operation{
		Tags:        []string{tag},
		Summary:     fmt.Sprintf("Custom: %s %s", doctype, ce.Path),
		OperationID: fmt.Sprintf("custom%s%s", doctype, sanitizeOpID(ce.Path)),
		Responses:   resps,
	}

	method := strings.ToUpper(ce.Method)
	if method == "POST" || method == "PUT" || method == "PATCH" {
		op.RequestBody = &RequestBody{
			Content: map[string]MediaType{
				"application/json": {Schema: &Schema{Type: "object"}},
			},
		}
	}

	switch method {
	case "GET":
		pi.Get = op
	case "POST":
		pi.Post = op
	case "PUT":
		pi.Put = op
	case "DELETE":
		pi.Delete = op
	case "PATCH":
		pi.Patch = op
	}
}

func addMethodPath(spec *OpenAPISpec, name string) {
	path := fmt.Sprintf("/api/v1/method/%s", name)
	pi := ensurePathItem(spec, path)

	resps := errorResponses()
	resps["200"] = Response{
		Description: "Method result",
		Content:     jsonContent,
	}

	pi.Get = &Operation{
		Tags:        []string{"Methods"},
		Summary:     fmt.Sprintf("Call method %s (GET)", name),
		OperationID: fmt.Sprintf("getMethod%s", sanitizeOpID(name)),
		Responses:   resps,
	}
	pi.Post = &Operation{
		Tags:        []string{"Methods"},
		Summary:     fmt.Sprintf("Call method %s (POST)", name),
		OperationID: fmt.Sprintf("postMethod%s", sanitizeOpID(name)),
		RequestBody: &RequestBody{
			Content: map[string]MediaType{
				"application/json": {Schema: &Schema{Type: "object"}},
			},
		},
		Responses: resps,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sanitizeOpID converts a path or method name into a valid operationId fragment.
// Replaces non-alphanumeric characters with underscores and trims leading slashes.
func sanitizeOpID(s string) string {
	s = strings.TrimPrefix(s, "/")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
