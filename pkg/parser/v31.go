package parser

import (
	"fmt"
)

// ConvertV31 transforms the raw AST of an OpenAPI 3.1.x document into the
// version-agnostic Spec model. It preserves source locations where possible.
// A returned error means the document could not be converted at all.
//
// Scalar field type mismatches are reported as warning diagnostics so that
// best-effort conversion can continue. Structural type mismatches (e.g., the
// info field being a string instead of a mapping) produce warning diagnostics.
// Additional warning diagnostics (for example for circular schema references)
// are collected during conversion and returned alongside the Spec.
func ConvertV31(root Node, opts ...ConvertOption) (*Spec, []Diagnostic, error) {
	if root == nil {
		return nil, []Diagnostic{{
			Severity: SeverityError,
			Summary:  "Missing OpenAPI document",
			Detail:   "ConvertV31 received a nil document root.",
		}}, fmt.Errorf("nil document root")
	}

	m, ok := root.(*MapNode)
	if !ok {
		rootLoc := nodeLoc(root)
		return nil, []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Invalid OpenAPI 3.1 document",
			Detail:         "Document root must be a JSON object or YAML mapping.",
			SourceLocation: &rootLoc,
		}}, fmt.Errorf("document root is %T, expected *MapNode", root)
	}

	cfg := defaultConvertConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	budget := NewBudget(cfg.limits)

	c := &v30Converter{version: Version3_1, budget: budget}
	spec := &Spec{SourceLocation: m.SourceLocation}

	if err := budget.Account(estimateNodeMemory(root)); err != nil {
		c.diags = append(c.diags, budgetExceededDiag(err, m.SourceLocation))
		return spec, c.diags, nil
	}

	forEachEntry(m, func(key string, value Node) {
		switch key {
		case "openapi":
			spec.OpenAPI = c.scalarString(value, "openapi")
		case "jsonSchemaDialect":
			spec.JSONSchemaDialect = c.scalarString(value, "jsonSchemaDialect")
		case "info":
			spec.Info = c.convertInfo(value)
		case "servers":
			spec.Servers = c.convertServers(value)
		case "paths":
			spec.Paths = c.convertPathItems(value)
		case "webhooks":
			spec.Webhooks = c.convertPathItems(value)
		case "components":
			spec.Components = c.convertComponents(value)
		case "security":
			spec.Security = c.convertSecurityRequirements(value)
		case "tags":
			spec.Tags = c.convertTags(value)
		case "externalDocs":
			spec.ExternalDocs = c.convertExternalDocs(value)
		}
	})

	circularRefs, circularDiags := DetectCircularSchemaRefs(root)
	c.diags = append(c.diags, circularDiags...)
	markCircularSchemaRefs(spec, circularRefs)

	spec.Extensions = nodeExtensions(root)
	return spec, c.diags, nil
}
