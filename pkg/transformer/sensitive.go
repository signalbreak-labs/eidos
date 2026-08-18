package transformer

import (
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// sensitiveSubstrings are name fragments that, when present in a string-typed
// attribute's wire name, strongly indicate the value is a secret. Matched by
// substring against the lowercased original name so both camelCase and
// snake_case forms are caught (httpPassword, http_password, sharedSecret,
// applicationSecretKey, fmpasswordEncrypted). The looksSensitive type guard
// excludes the boolean/integer false positives these substrings also occur in
// (allowBlankPassword, minPasswordLen, passwordSet, privateKey-as-boolean).
var sensitiveSubstrings = []string{
	"password",
	"passwd",
	"secret",
	"apikey",
	"api_key",
	"privatekey",
	"private_key",
	"accesskey",
	"access_key",
	"secretkey",
	"secret_key",
	"clientsecret",
	"client_secret",
	"credential",
}

// looksSensitive reports whether an attribute's name marks its value as a secret
// that should be Sensitive in Terraform state. It combines a substring check
// (password/secret/api_key/...) with a token-suffix rule: a name whose final
// snake_case segment is "token" (bare "token", access_token, auth_token,
// key_token) is sensitive, while token_name/token_id/token_type are not. Only
// string-typed scalar attributes are considered; booleans, integers, arrays,
// and objects are excluded so that flag fields like allowBlankPassword or
// passwordSet are not redacted. An explicit OpenAPI format: "password" is also
// honored (the ephemeral result builder already does this; applying it here
// makes resources and data sources consistent).
func looksSensitive(name string, schema ir.SchemaIR) bool {
	// Only scalar strings can carry a secret. Collections, objects, unions,
	// and non-string primitives are excluded so we never redact a whole
	// object or a boolean flag such as allowBlankPassword/passwordSet.
	if schema.Type != ir.TypeString && schema.Type != "" {
		return false
	}
	if schema.Collection != nil || schema.Union != nil ||
		len(schema.Attributes) > 0 || len(schema.Blocks) > 0 {
		return false
	}
	if strings.EqualFold(schema.Format, "password") {
		return true
	}
	lower := strings.ToLower(name)
	for _, kw := range sensitiveSubstrings {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	// Token rule: the final snake_case segment is "token". Catches bare
	// "token", access_token, auth_token, key_token; excludes token_name,
	// token_id, token_type.
	segs := strings.Split(lower, "_")
	if len(segs) > 0 && segs[len(segs)-1] == "token" {
		return true
	}
	return false
}

// attrWireName returns the original OpenAPI property name for an attribute,
// falling back to its Terraform name when no wire name was recorded.
func attrWireName(attr ir.AttributeIR) string {
	if attr.WireName != "" {
		return attr.WireName
	}
	return attr.Name
}

// InferSensitiveAttributes walks obj's attributes and marks string-typed
// attributes whose name indicates a secret (password/secret/token/...) as
// Sensitive, recursing into nested object schemas, blocks, collection element
// types, and union variants. It only ever adds Sensitive; attributes already
// marked Sensitive (by security-scheme inference, write-only processing, or an
// override) are left untouched.
//
// This catches secrets the spec does not flag with writeOnly/format=password
// but names clearly (e.g. Gigamon's httpPassword, secretKey, accessKey) so they
// are hidden in Terraform state rather than leaked in plan/refresh output. It
// must only be applied to schema kinds the generator renders Sensitive for
// (resources, data sources, ephemeral resources, provider config). For schema
// kinds that ignore Sensitive (actions, list resources), call
// WarnUnmarkableSensitive instead so the limitation is surfaced, not dropped
// silently (fail-loud).
func InferSensitiveAttributes(obj *ir.ObjectSchemaIR) {
	if obj == nil {
		return
	}
	for i := range obj.Attributes {
		attr := &obj.Attributes[i]
		if !attr.Sensitive && looksSensitive(attrWireName(*attr), attr.Schema) {
			// AttributeIR is the single source of truth for attribute flags; the
			// embedded SchemaIR no longer duplicates them (N-49).
			attr.Sensitive = true
		}
		inferSensitiveRecursive(&attr.Schema)
	}
	for i := range obj.Blocks {
		InferSensitiveAttributes(&obj.Blocks[i].Schema)
	}
}

// inferSensitiveRecursive descends into the nested schema nodes reachable from
// schema (object aggregates, collection element types, union variants, and the
// JSON Schema conditional/pattern siblings) applying InferSensitiveAttributes
// to each object-like node. It mirrors applyWriteOnlyRecursive's traversal.
func inferSensitiveRecursive(schema *ir.SchemaIR) {
	if schema == nil {
		return
	}

	if len(schema.Attributes) > 0 || len(schema.Blocks) > 0 {
		obj := ir.ObjectSchemaIR{
			Attributes: schema.Attributes,
			Blocks:     schema.Blocks,
		}
		InferSensitiveAttributes(&obj)
		schema.Attributes = obj.Attributes
		schema.Blocks = obj.Blocks
	}

	if schema.Collection != nil {
		inferSensitiveRecursive(&schema.Collection.ElementType)
	}

	if schema.Union != nil {
		for i := range schema.Union.Variants {
			inferSensitiveRecursive(&schema.Union.Variants[i])
		}
	}

	if schema.Not != nil {
		inferSensitiveRecursive(schema.Not)
	}
	if schema.IfSchema != nil {
		inferSensitiveRecursive(schema.IfSchema)
	}
	if schema.ThenSchema != nil {
		inferSensitiveRecursive(schema.ThenSchema)
	}
	if schema.ElseSchema != nil {
		inferSensitiveRecursive(schema.ElseSchema)
	}
	for _, dep := range schema.DependentSchemas {
		inferSensitiveRecursive(dep)
	}
	for _, pp := range schema.PatternProperties {
		inferSensitiveRecursive(pp)
	}
	if schema.PropertyNames != nil {
		inferSensitiveRecursive(schema.PropertyNames)
	}
	if schema.UnevaluatedProperties != nil {
		inferSensitiveRecursive(schema.UnevaluatedProperties)
	}
}

// WarnUnmarkableSensitive scans obj for string-typed attributes whose names
// indicate a secret but whose schema kind cannot render Sensitive (action
// config attributes per action.go, list resource schema attributes per
// list.go). It appends one Warning per such attribute so the limitation is
// surfaced to the practitioner rather than the secret leaking in state with no
// signal. It does not modify the schema. Recurse matches InferSensitiveAttributes.
func WarnUnmarkableSensitive(obj *ir.ObjectSchemaIR, kind, name string, diags *diagnostics.Diagnostics) {
	if obj == nil || diags == nil {
		return
	}
	for i := range obj.Attributes {
		attr := &obj.Attributes[i]
		if looksSensitive(attrWireName(*attr), attr.Schema) {
			*diags = append(*diags, diagnostics.Diagnostic{
				Severity: diagnostics.Warning,
				Summary:  "Secret-named attribute cannot be marked Sensitive",
				Detail: kind + " " + name + " has a string attribute " + attrWireName(*attr) +
					" whose name indicates a secret, but " + kind +
					" schema attributes do not support Sensitive in the generated provider" +
					" (see pkg/generator action.go/list.go). The value will appear in state;" +
					" avoid storing real secrets in this attribute or override the type via generator.yaml.",
			})
		}
		warnUnmarkableSensitiveRecursive(&attr.Schema, kind, name, diags)
	}
	for i := range obj.Blocks {
		WarnUnmarkableSensitive(&obj.Blocks[i].Schema, kind, name, diags)
	}
}

func warnUnmarkableSensitiveRecursive(schema *ir.SchemaIR, kind, name string, diags *diagnostics.Diagnostics) {
	if schema == nil {
		return
	}
	if len(schema.Attributes) > 0 || len(schema.Blocks) > 0 {
		obj := ir.ObjectSchemaIR{Attributes: schema.Attributes, Blocks: schema.Blocks}
		WarnUnmarkableSensitive(&obj, kind, name, diags)
		schema.Attributes = obj.Attributes
		schema.Blocks = obj.Blocks
	}
	if schema.Collection != nil {
		warnUnmarkableSensitiveRecursive(&schema.Collection.ElementType, kind, name, diags)
	}
	if schema.Union != nil {
		for i := range schema.Union.Variants {
			warnUnmarkableSensitiveRecursive(&schema.Union.Variants[i], kind, name, diags)
		}
	}
}
