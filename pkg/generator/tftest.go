package generator

import (
	"fmt"
	"path/filepath"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TerraformTestFile returns the generated tests/<name>.tftest.hcl orchestration
// file for a single managed resource. The file contains a single run block that
// applies the supporting test module and asserts that the generated resource id
// matches the placeholder value produced by the generated provider's stub
// Create implementation.
func TerraformTestFile(pir ir.ProviderIR, r ir.ResourceIR, _ BuildConfig) File {
	path := fmt.Sprintf("tests/%s.tftest.hcl", snakeCase(r.Name))
	return staticFile(path, generateTerraformTestHCL(pir, r))
}

// TerraformTestModuleFile returns the generated tests/modules/<name>/main.tf
// module for a single managed resource. The module declares required_providers,
// configures the generated provider with dummy values, exposes variables for
// the resource's required primitive attributes, and emits the resource under
// test along with an output for its identifier.
func TerraformTestModuleFile(pir ir.ProviderIR, r ir.ResourceIR, cfg BuildConfig) File {
	path := filepath.Join("tests", "modules", snakeCase(r.Name), "main.tf")
	return staticFile(path, generateTerraformTestModuleHCL(pir, r, cfg))
}

// TerraformTestFiles returns the complete set of generated Terraform native
// test files for a provider. For each resource it emits both the
// .tftest.hcl orchestration file and the supporting module file.
func TerraformTestFiles(pir ir.ProviderIR, cfg BuildConfig) []File {
	files := make([]File, 0, len(pir.Resources)*2)
	for _, r := range pir.Resources {
		files = append(files, TerraformTestFile(pir, r, cfg), TerraformTestModuleFile(pir, r, cfg))
	}
	return files
}

// generateTerraformTestHCL builds the .tftest.hcl body for a resource.
func generateTerraformTestHCL(_ ir.ProviderIR, r ir.ResourceIR) string {
	var h hclBuilder

	runName := fmt.Sprintf("create_%s", snakeCase(r.Name))
	h.writeLinef(`run %q {`, runName)
	h.indent++
	h.writeLinef("command = apply")

	requiredPrimitiveAttrs := terraformTestRequiredPrimitiveAttributes(r.Schema)
	if len(requiredPrimitiveAttrs) > 0 {
		h.writeLinef("")
		h.writeLinef("variables {")
		h.indent++
		for _, attr := range requiredPrimitiveAttrs {
			h.writeLinef("%s = %s", attr.Name, terraformTestVariableValue(attr))
		}
		h.indent--
		h.writeLinef("}")
	}

	h.writeLinef("")
	h.writeLinef("module {")
	h.indent++
	h.writeLinef("source = \"./tests/modules/%s\"", snakeCase(r.Name))
	h.indent--
	h.writeLinef("}")

	idInfo := resourceIDFieldInfo(r)
	if idInfo.found {
		h.writeLinef("")
		h.writeLinef("assert {")
		h.indent++
		h.writeLinef("condition     = output.%s == %s", idInfo.attr, terraformTestExpectedIDValue(idInfo.primitive))
		h.writeLinef("error_message = \"unexpected %s\"", idInfo.attr)
		h.indent--
		h.writeLinef("}")
	}

	h.indent--
	h.writeLinef("}")
	return h.b.String()
}

// generateTerraformTestModuleHCL builds the supporting module main.tf body.
func generateTerraformTestModuleHCL(pir ir.ProviderIR, r ir.ResourceIR, cfg BuildConfig) string {
	var h hclBuilder

	localName := providerTypeName(pir)
	providerSource := fmt.Sprintf("%s/%s", cfg.namespace(), cfg.providerName())

	h.writeLinef("terraform {")
	h.indent++
	h.writeLinef("required_providers {")
	h.indent++
	h.writeLinef("%s = {", localName)
	h.indent++
	h.writeLinef("source = \"%s\"", providerSource)
	h.indent--
	h.writeLinef("}")
	h.indent--
	h.writeLinef("}")
	h.indent--
	h.writeLinef("}")

	h.writeLinef("")
	h.writeLinef("provider \"%s\" {", localName)
	h.indent++
	writeTerraformTestProviderConfig(&h, pir)
	h.indent--
	h.writeLinef("}")

	requiredPrimitiveAttrs := terraformTestRequiredPrimitiveAttributes(r.Schema)
	for _, attr := range requiredPrimitiveAttrs {
		h.writeLinef("")
		h.writeLinef("variable \"%s\" {", attr.Name)
		h.indent++
		h.writeLinef("type = %s", terraformTestVariableType(attr))
		h.indent--
		h.writeLinef("}")
	}

	h.writeLinef("")
	h.writeLinef(`resource "%s" "example" {`, resourceTypeName(r))
	h.indent++
	writeTerraformTestResourceBody(&h, r.Schema, true)
	h.indent--
	h.writeLinef("}")

	idInfo := resourceIDFieldInfo(r)
	if idInfo.found {
		h.writeLinef("")
		h.writeLinef("output \"%s\" {", idInfo.attr)
		h.indent++
		h.writeLinef("value = %s.example.%s", resourceTypeName(r), idInfo.attr)
		h.indent--
		h.writeLinef("}")
	}

	return h.b.String()
}

// writeTerraformTestProviderConfig writes dummy values for all required
// provider configuration attributes. The generated provider is a stub, so simple
// placeholder values are sufficient for the test to plan and apply.
func writeTerraformTestProviderConfig(h *hclBuilder, pir ir.ProviderIR) {
	for _, attr := range pir.ConfigSchema.Attributes {
		if !attr.Required {
			continue
		}
		writeTerraformTestProviderAttribute(h, attr)
	}
	for _, block := range pir.ConfigSchema.Blocks {
		writeTerraformTestProviderBlock(h, block)
	}
}

func writeTerraformTestProviderAttribute(h *hclBuilder, attr ir.AttributeIR) {
	s := attr.Schema

	if s.Collection != nil {
		writeTerraformTestCollectionAttribute(h, attr, false)
		return
	}

	if isObjectLike(s) {
		h.writeLinef("%s = {", attr.Name)
		h.indent++
		writeTerraformTestProviderBody(h, ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks})
		h.indent--
		h.writeLinef("}")
		return
	}

	h.writeLinef("%s = %s", attr.Name, terraformTestPrimitiveValue(s.Type))
}

func writeTerraformTestProviderBody(h *hclBuilder, schema ir.ObjectSchemaIR) {
	for _, attr := range schema.Attributes {
		writeTerraformTestProviderAttribute(h, attr)
	}
	for _, block := range schema.Blocks {
		writeTerraformTestProviderBlock(h, block)
	}
}

func writeTerraformTestProviderBlock(h *hclBuilder, block ir.BlockIR) {
	h.writeLinef("%s {", block.Name)
	h.indent++
	writeTerraformTestProviderBody(h, block.Schema)
	h.indent--
	h.writeLinef("}")
}

// writeTerraformTestResourceBody renders the resource block. Only required
// attributes are emitted because Computed-only values are supplied by the
// provider during apply. Required primitive attributes are assigned from
// variables so each test run can vary them; required object-like and collection
// attributes use inline placeholder values. If a future change needs per-test
// configuration of Optional attributes, this policy will need to be revisited.
//
// useVars controls whether required primitives are rendered as var.<name>
// references. Only the top-level call passes true: variables are declared only
// for direct top-level required primitives (terraformTestRequiredPrimitive
// Attributes), so nested primitives — inside objects, blocks, or collections —
// must use inline placeholder values to avoid "Reference to undeclared
// variable" errors (M-55).
func writeTerraformTestResourceBody(h *hclBuilder, schema ir.ObjectSchemaIR, useVars bool) {
	for _, attr := range schema.Attributes {
		if !attr.Required {
			continue
		}
		if attr.Schema.Collection != nil || isObjectLike(attr.Schema) {
			writeTerraformTestResourceAttribute(h, attr)
			continue
		}
		if useVars {
			h.writeLinef("%s = var.%s", attr.Name, attr.Name)
		} else {
			h.writeLinef("%s = %s", attr.Name, terraformTestPrimitiveValue(attr.Schema.Type))
		}
	}
	for _, block := range schema.Blocks {
		if block.Schema.Attributes == nil && block.Schema.Blocks == nil {
			continue
		}
		writeTerraformTestResourceBlock(h, block)
	}
}

func writeTerraformTestResourceAttribute(h *hclBuilder, attr ir.AttributeIR) {
	s := attr.Schema

	if s.Collection != nil {
		writeTerraformTestCollectionAttribute(h, attr, true)
		return
	}

	if isObjectLike(s) {
		h.writeLinef("%s {", attr.Name)
		h.indent++
		// Nested object attributes are never wired to variables (M-55).
		writeTerraformTestResourceBody(h, ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks}, false)
		h.indent--
		h.writeLinef("}")
		return
	}

	h.writeLinef("%s = %s", attr.Name, terraformTestPrimitiveValue(s.Type))
}

func writeTerraformTestResourceBlock(h *hclBuilder, block ir.BlockIR) {
	h.writeLinef("%s {", block.Name)
	h.indent++
	// Block contents are nested and never wired to variables (M-55).
	writeTerraformTestResourceBody(h, block.Schema, false)
	h.indent--
	h.writeLinef("}")
}

func writeTerraformTestCollectionAttribute(h *hclBuilder, attr ir.AttributeIR, _ bool) {
	s := attr.Schema
	elem := s.Collection.ElementType

	switch s.Collection.Kind {
	case ir.List, ir.Set:
		if isPrimitiveSchema(elem) {
			h.writeLinef("%s = [ %s ]", attr.Name, terraformTestPrimitiveValue(elem.Type))
			return
		}
		if isObjectLike(elem) {
			h.writeLinef("%s = [{", attr.Name)
			h.indent++
			// Collection elements are nested and never wired to variables (M-55).
			writeTerraformTestResourceBody(h, ir.ObjectSchemaIR{Attributes: elem.Attributes, Blocks: elem.Blocks}, false)
			h.indent--
			h.writeLinef("}]")
			return
		}
	case ir.Map:
		if isPrimitiveSchema(elem) {
			h.writeLinef("%s = {", attr.Name)
			h.indent++
			h.writeLinef("\"key\" = %s", terraformTestPrimitiveValue(elem.Type))
			h.indent--
			h.writeLinef("}")
			return
		}
		if isObjectLike(elem) {
			h.writeLinef("%s = {", attr.Name)
			h.indent++
			h.writeLinef("\"key\" = {")
			h.indent++
			// Collection elements are nested and never wired to variables (M-55).
			writeTerraformTestResourceBody(h, ir.ObjectSchemaIR{Attributes: elem.Attributes, Blocks: elem.Blocks}, false)
			h.indent--
			h.writeLinef("}")
			h.indent--
			h.writeLinef("}")
			return
		}
	}

	// Fallback for collection element types that are neither primitive nor
	// object-like. Emitting an empty list silently hides the unsupported schema;
	// emitting a placeholder object literal makes the gap visible in the generated
	// test module and surfaces a clear Terraform error if the element type is not
	// actually an object.
	h.writeLinef("%s = [{}]", attr.Name)
}

// terraformTestRequiredPrimitiveAttributes returns the primitive (or primitive
// collection) attributes that are required by the schema and can be supplied
// through a Terraform variable.
func terraformTestRequiredPrimitiveAttributes(schema ir.ObjectSchemaIR) []ir.AttributeIR {
	var attrs []ir.AttributeIR
	for _, attr := range schema.Attributes {
		if !attr.Required {
			continue
		}
		if attr.Schema.Collection != nil {
			if isPrimitiveSchema(attr.Schema.Collection.ElementType) {
				attrs = append(attrs, attr)
			}
			continue
		}
		if isPrimitiveSchema(attr.Schema) {
			attrs = append(attrs, attr)
		}
	}
	return attrs
}

// terraformTestVariableType returns the HCL type expression for a variable
// declaration that matches the attribute's schema.
func terraformTestVariableType(attr ir.AttributeIR) string {
	s := attr.Schema
	if s.Collection != nil {
		elem := s.Collection.ElementType
		switch s.Collection.Kind {
		case ir.List:
			return fmt.Sprintf("list(%s)", terraformTestPrimitiveTypeName(elem.Type))
		case ir.Set:
			return fmt.Sprintf("set(%s)", terraformTestPrimitiveTypeName(elem.Type))
		case ir.Map:
			return fmt.Sprintf("map(%s)", terraformTestPrimitiveTypeName(elem.Type))
		}
	}
	return terraformTestPrimitiveTypeName(s.Type)
}

func terraformTestPrimitiveTypeName(t ir.PrimitiveType) string {
	switch t {
	case ir.TypeString:
		return "string"
	case ir.TypeInt, ir.TypeFloat:
		return "number"
	case ir.TypeBool:
		return "bool"
	case ir.TypeDynamic:
		return "any"
	default:
		return "string"
	}
}

// terraformTestVariableValue returns a concrete HCL literal suitable for the
// variables block in a .tftest.hcl run block.
func terraformTestVariableValue(attr ir.AttributeIR) string {
	s := attr.Schema
	if s.Collection != nil {
		elem := s.Collection.ElementType
		switch s.Collection.Kind {
		case ir.List, ir.Set:
			if isPrimitiveSchema(elem) {
				return fmt.Sprintf("[ %s ]", terraformTestPrimitiveValue(elem.Type))
			}
		case ir.Map:
			if isPrimitiveSchema(elem) {
				return fmt.Sprintf("{ key = %s }", terraformTestPrimitiveValue(elem.Type))
			}
		}
		return "[]"
	}
	return terraformTestPrimitiveValue(s.Type)
}

// terraformTestPrimitiveValue returns a deterministic placeholder literal for
// a primitive HCL type. It delegates to primitiveExampleValue so the test
// module and the generated examples always use the same placeholder values.
func terraformTestPrimitiveValue(t ir.PrimitiveType) string {
	return primitiveExampleValue(t)
}

// terraformTestExpectedIDValue returns the HCL literal expected for the
// resource's identifier after the generated stub Create runs. It delegates to
// createIDPlaceholder in resource.go so the test assertion and the provider's
// Create implementation always agree.
func terraformTestExpectedIDValue(t ir.PrimitiveType) string {
	return createIDPlaceholder(t)
}
