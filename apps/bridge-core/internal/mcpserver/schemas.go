package mcpserver

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
)

// The tool input schemas below mirror, field for field, the JSON Schemas
// that xsschema 0.4.x derives from the zod schemas in
// src/mcp/tools/*.ts (draft-07, additionalProperties: false). They were
// extracted by running the TS build's own xsschema over the zod schemas;
// testdata/tools_list_ts.json pins the full tools/list payload.

const jsonSchemaDraft07 = "http://json-schema.org/draft-07/schema#"

func float64Ptr(f float64) *float64 { return &f }

func intPtr(i int) *int { return &i }

// falseSchema marshals as JSON Schema `false` (no value validates); the
// jsonschema-go spelling of additionalProperties: false.
func falseSchema() *jsonschema.Schema { return &jsonschema.Schema{Not: &jsonschema.Schema{}} }

// objectSchema assembles the shared envelope every tool schema uses:
// type object, closed (additionalProperties false), draft-07 dialect.
// propertyOrder preserves the zod declaration order on the wire, matching
// xsschema's emission order.
func objectSchema(propertyOrder []string, required []string, properties map[string]*jsonschema.Schema) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:                 "object",
		Properties:           properties,
		Required:             required,
		AdditionalProperties: falseSchema(),
		Schema:               jsonSchemaDraft07,
		PropertyOrder:        propertyOrder,
	}
}

// Shared property shapes.
var (
	timeoutMSProperty = &jsonschema.Schema{Type: "integer", Minimum: float64Ptr(100), Maximum: float64Ptr(120000)}
	tabIDProperty     = &jsonschema.Schema{Type: "integer", Minimum: float64Ptr(0)}
	selectorProperty  = &jsonschema.Schema{Type: "string", MinLength: intPtr(1)}
)

// withTabAndTimeout is the common tail of the selector/tab-taking schemas:
// tab_id (required) then timeout_ms (optional), both declared in the zod
// sources in that order.
func withTabAndTimeout(orderPrefix, requiredPrefix []string, properties map[string]*jsonschema.Schema) (order []string, required []string, props map[string]*jsonschema.Schema) {
	order = append(append([]string{}, orderPrefix...), "tab_id", "timeout_ms")
	required = append(append([]string{}, requiredPrefix...), "tab_id")
	props = make(map[string]*jsonschema.Schema, len(properties)+2)
	for k, v := range properties {
		props[k] = v
	}
	props["tab_id"] = tabIDProperty
	props["timeout_ms"] = timeoutMSProperty
	return order, required, props
}

func selectorTabTimeoutSchema() *jsonschema.Schema {
	order, required, props := withTabAndTimeout([]string{"selector"}, []string{"selector"}, map[string]*jsonschema.Schema{
		"selector": selectorProperty,
	})
	return objectSchema(order, required, props)
}

func tabIDTimeoutSchema() *jsonschema.Schema {
	order, required, props := withTabAndTimeout(nil, nil, nil)
	return objectSchema(order, required, props)
}

func timeoutOnlySchema() *jsonschema.Schema {
	return objectSchema([]string{"timeout_ms"}, nil, map[string]*jsonschema.Schema{
		"timeout_ms": timeoutMSProperty,
	})
}

var toolInputSchemas = map[string]*jsonschema.Schema{
	"list_browsers": timeoutOnlySchema(),
	"set_browser": objectSchema(
		[]string{"browserId"},
		[]string{"browserId"},
		map[string]*jsonschema.Schema{
			"browserId": {Type: "string", MinLength: intPtr(1)},
		},
	),
	"navigate": objectSchema(
		[]string{"url", "tab_id", "timeout_ms"},
		[]string{"url", "tab_id"},
		map[string]*jsonschema.Schema{
			"url":        {Type: "string", Format: "uri"},
			"tab_id":     tabIDProperty,
			"timeout_ms": timeoutMSProperty,
		},
	),
	"go_back":    tabIDTimeoutSchema(),
	"go_forward": tabIDTimeoutSchema(),
	"refresh":    tabIDTimeoutSchema(),
	"tab_list":   timeoutOnlySchema(),
	"tab_new": objectSchema(
		[]string{"url", "active", "auto_close", "timeout_ms"},
		nil,
		map[string]*jsonschema.Schema{
			"url":        {Type: "string", Format: "uri"},
			"active":     {Type: "boolean"},
			"auto_close": {Type: "boolean"},
			"timeout_ms": timeoutMSProperty,
		},
	),
	"tab_close":  tabIDTimeoutSchema(),
	"tab_switch": tabIDTimeoutSchema(),
	"click":      selectorTabTimeoutSchema(),
	"type": objectSchema(
		[]string{"selector", "text", "submit", "tab_id", "timeout_ms"},
		[]string{"selector", "text", "tab_id"},
		map[string]*jsonschema.Schema{
			"selector":   selectorProperty,
			"text":       {Type: "string"},
			"submit":     {Type: "boolean"},
			"tab_id":     tabIDProperty,
			"timeout_ms": timeoutMSProperty,
		},
	),
	"select": objectSchema(
		[]string{"selector", "value", "tab_id", "timeout_ms"},
		[]string{"selector", "value", "tab_id"},
		map[string]*jsonschema.Schema{
			"selector":   selectorProperty,
			"value":      {Type: "string", MinLength: intPtr(1)},
			"tab_id":     tabIDProperty,
			"timeout_ms": timeoutMSProperty,
		},
	),
	"scroll": objectSchema(
		[]string{"x", "y", "selector", "tab_id", "timeout_ms"},
		[]string{"x", "y", "tab_id"},
		map[string]*jsonschema.Schema{
			"x":          {Type: "integer"},
			"y":          {Type: "integer"},
			"selector":   {Type: "string", MinLength: intPtr(1), Default: json.RawMessage(`"page"`)},
			"tab_id":     tabIDProperty,
			"timeout_ms": timeoutMSProperty,
		},
	),
	"hover":    selectorTabTimeoutSchema(),
	"get_text": selectorTabTimeoutSchema(),
	"get_html": selectorTabTimeoutSchema(),
	"snapshot": objectSchema(
		[]string{"selector", "filter", "max_chars", "tab_id", "timeout_ms"},
		[]string{"tab_id"},
		map[string]*jsonschema.Schema{
			"selector":   selectorProperty,
			"filter":     {Type: "string", Enum: []any{"interactive", "full"}},
			"max_chars":  {Type: "integer", Minimum: float64Ptr(100), Maximum: float64Ptr(100000)},
			"tab_id":     tabIDProperty,
			"timeout_ms": timeoutMSProperty,
		},
	),
	"screenshot": objectSchema(
		[]string{"fullPage", "tab_id", "timeout_ms"},
		[]string{"tab_id"},
		map[string]*jsonschema.Schema{
			"fullPage":   {Type: "boolean"},
			"tab_id":     tabIDProperty,
			"timeout_ms": timeoutMSProperty,
		},
	),
	"pageinfo":        tabIDTimeoutSchema(),
	"wait_element":    selectorTabTimeoutSchema(),
	"wait_navigation": tabIDTimeoutSchema(),
}
