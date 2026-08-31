package schema

import (
	"fmt"
	"go/ast"
	"sort"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// planModifierKindPackage maps the attribute kind used by the resource
// attribute builders (the same kind strings AddValidators takes) to the
// framework's typed plan-modifier package for that attribute type. Every
// attribute kind has a matching package because plan modifiers are typed per
// attribute type in the plugin framework: a StringAttribute takes
// []planmodifier.String and stringplanmodifier.RequiresReplace(), an
// Int64Attribute takes []planmodifier.Int64, and so on.
var planModifierKindPackage = map[string]string{
	"String":  "stringplanmodifier",
	"Int64":   "int64planmodifier",
	"Float64": "float64planmodifier",
	"Bool":    "boolplanmodifier",
	"Dynamic": "dynamicplanmodifier",
	"List":    "listplanmodifier",
	"Set":     "setplanmodifier",
	"Map":     "mapplanmodifier",
	"Object":  "objectplanmodifier",
}

// AddPlanModifiers adds the Terraform-plugin-framework PlanModifiers field to
// elems when the attribute carries plan-modifier IR entries or an explicit
// force_new override. kind is the plan-modifier interface name (String, Int64,
// Float64, Bool, Dynamic, List, Set, Map, Object) and selects both the typed
// slice ([]planmodifier.<kind>) and the typed constructor package. It is the
// plan-modifier counterpart of AddValidators and is called at the same sites.
//
// An IR plan modifier whose typed package does not match the attribute kind
// (e.g. "stringplanmodifier.UseStateForUnknown" on an Int64 attribute), an
// argument-bearing constructor (the static-default modifiers the IR models
// forward-compatibly but no producer emits today), or an unrecognized
// constructor panics: the surrounding file render catches panics as render
// errors (renderFileSafely), so the mismatch fails loud instead of silently
// emitting an attribute whose declared modifiers never applied (H-15).
func AddPlanModifiers(elems []ast.Expr, attr ir.AttributeIR, kind string) []ast.Expr {
	typedPkg, ok := planModifierKindPackage[kind]
	if !ok {
		panic(fmt.Sprintf("plan modifiers: attribute kind %q has no typed plan-modifier package", kind))
	}
	exprs := planModifierExprs(attr, typedPkg)
	if len(exprs) == 0 {
		return elems
	}
	return append(elems, astgen.KeyValueExpr(
		astgen.Ident("PlanModifiers"),
		astgen.CompositeLit(
			astgen.SliceType(astgen.QualExpr("planmodifier", kind)),
			exprs...,
		),
	))
}

// planModifierExprs returns the plan-modifier constructor calls that apply to
// the attribute: the explicit force_new override first (so a force_new entry
// always yields the leading RequiresReplace), then the IR entries in order.
// RequiresReplace is stored once, generically, in the IR
// (PlanModifierTypeRequiresReplace) because force_new is type-agnostic; the
// emission site resolves it to the typed constructor the framework requires.
func planModifierExprs(attr ir.AttributeIR, typedPkg string) []ast.Expr {
	var exprs []ast.Expr
	if attr.ForceNew {
		exprs = append(exprs, requiresReplaceExpr(typedPkg))
	}
	for _, pm := range attr.PlanModifiers {
		switch {
		case pm.Type == ir.PlanModifierTypeRequiresReplace:
			exprs = append(exprs, requiresReplaceExpr(typedPkg))
		case len(pm.Args) > 0:
			panic(fmt.Sprintf("plan modifier %q carries arguments; argument-bearing modifiers (static defaults) have no emission path", pm.Type))
		default:
			pkg, fn, ok := splitPlanModifierConstructor(pm.Type)
			if !ok {
				panic(fmt.Sprintf("plan modifier type %q is not a <package>.<Constructor> call", pm.Type))
			}
			if pkg != typedPkg {
				panic(fmt.Sprintf("plan modifier %q does not match attribute type package %q", pm.Type, typedPkg))
			}
			if fn != "UseStateForUnknown" {
				panic(fmt.Sprintf("plan modifier constructor %q is not supported; only RequiresReplace and UseStateForUnknown are emitted", pm.Type))
			}
			exprs = append(exprs, astgen.Call(astgen.QualExpr(pkg, fn)))
		}
	}
	return exprs
}

// requiresReplaceExpr returns the typed RequiresReplace() constructor call for
// the attribute's plan-modifier package.
func requiresReplaceExpr(typedPkg string) ast.Expr {
	return astgen.Call(astgen.QualExpr(typedPkg, "RequiresReplace"))
}

// splitPlanModifierConstructor splits an IR plan-modifier type into its
// <package>.<Constructor> halves, reporting ok=false for bare names.
func splitPlanModifierConstructor(typ string) (pkg, fn string, ok bool) {
	for i := len(typ) - 1; i >= 0; i-- {
		if typ[i] == '.' {
			return typ[:i], typ[i+1:], true
		}
	}
	return "", "", false
}

// UsedPlanModifierImports inspects rendered schema expressions and returns
// the plan-modifier imports they actually reference: the shared planmodifier
// interface package (which names the PlanModifiers slice type) plus each
// typed package whose constructors appear. Deriving the imports from the
// rendered AST rather than the IR keeps them exact: an attribute that the
// renderer drops (e.g. a nested dynamic inside a collection) contributes no
// import, so the generated file never carries an unused import.
func UsedPlanModifierImports(exprs []ast.Expr) [][2]string {
	typed := map[string]bool{}
	shared := false
	for _, expr := range exprs {
		ast.Inspect(expr, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if ident.Name == "planmodifier" {
				shared = true
				return true
			}
			for _, pkg := range planModifierKindPackage {
				if ident.Name == pkg {
					typed[pkg] = true
				}
			}
			return true
		})
	}
	if !shared && len(typed) == 0 {
		return nil
	}
	pkgs := make([]string, 0, len(typed))
	for pkg := range typed {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	imports := make([][2]string, 0, len(pkgs)+1)
	imports = append(imports, [2]string{"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier", "planmodifier"})
	for _, pkg := range pkgs {
		imports = append(imports, [2]string{
			"github.com/hashicorp/terraform-plugin-framework/resource/schema/" + pkg,
			pkg,
		})
	}
	return imports
}
