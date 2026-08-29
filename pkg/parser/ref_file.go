package parser

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxReferenceDocuments = 100
	maxReferenceBytes     = 50 << 20
	maxReferenceDepth     = 100
)

type localRefResolver struct {
	version    Version
	entryPath  string
	documents  map[string]Node
	aliases    map[string]string
	typed      map[string]resolvedReference
	totalBytes int64
	produces   []string
	consumes   []string
	schemes    []string
}

type resolvedReference struct {
	value any
}

// EnableLocalReferences lets spec resolve relative file references from a
// locally loaded entry document. Callers must not enable it for inline or
// remotely fetched documents: the entry path is the trust boundary that makes
// filesystem resolution explicit.
func EnableLocalReferences(spec *Spec, root Node, entryPath string, version Version) error {
	if spec == nil || root == nil {
		return fmt.Errorf("cannot enable local references without a spec and document root")
	}
	canonical, err := canonicalReferencePath(entryPath)
	if err != nil {
		return fmt.Errorf("resolve entry spec path: %w", err)
	}
	var size int64
	if info, statErr := os.Stat(canonical); statErr == nil {
		size = info.Size()
	}
	resolver := &localRefResolver{
		version:    version,
		entryPath:  canonical,
		documents:  map[string]Node{canonical: root},
		aliases:    map[string]string{root.GetSourceLocation().File: canonical, canonical: canonical},
		typed:      make(map[string]resolvedReference),
		totalBytes: size,
	}
	if version == Version2_0 {
		if mapping, ok := root.(*MapNode); ok {
			var diags []Diagnostic
			resolver.produces = v2StringSlice(findEntryValue(mapping, "produces"), "produces", &diags)
			resolver.consumes = v2StringSlice(findEntryValue(mapping, "consumes"), "consumes", &diags)
			resolver.schemes = v2StringSlice(findEntryValue(mapping, "schemes"), "schemes", &diags)
		}
	}
	spec.localRefs = resolver
	return nil
}

// ReferenceKey returns a document-qualified identity for ref. It is used by
// schema traversal to terminate cross-file cycles whose raw relative strings
// differ at each hop.
func (spec *Spec) ReferenceKey(ref string, loc SourceLocation) string {
	if spec == nil || spec.localRefs == nil || ref == "" || !spec.localRefs.shouldResolve(ref, loc) {
		return ref
	}
	_, key, _ := spec.localRefs.resolve(ref, loc)
	if key == "" {
		return ref
	}
	return key
}

// ResolveSchemaReference resolves schema's $ref, falling back to the existing
// same-document component lookup when local-file resolution is not enabled.
func (spec *Spec) ResolveSchemaReference(schema *Schema) (*Schema, []Diagnostic) {
	if schema == nil || schema.Ref == "" || spec == nil {
		return schema, nil
	}
	if spec.localRefs != nil && spec.localRefs.shouldResolve(schema.Ref, schema.SourceLocation) {
		value, diags := spec.localRefs.resolveTyped("schema", schema.Ref, schema.SourceLocation, spec.localRefs.convertSchema)
		if resolved, ok := value.(*Schema); ok && resolved != nil {
			return resolved, diags
		}
		return schema, diags
	}
	if spec.Components != nil {
		if resolved := spec.Components.Schemas[referenceBaseName(schema.Ref)]; resolved != nil {
			return resolved, nil
		}
	}
	return schema, nil
}

// ResolveParameterReference resolves a Parameter Reference Object.
func (spec *Spec) ResolveParameterReference(parameter *Parameter) (*Parameter, []Diagnostic) {
	if parameter == nil || parameter.Ref == "" || spec == nil {
		return parameter, nil
	}
	current := parameter
	seen := make(map[string]bool)
	var allDiags []Diagnostic
	for current.Ref != "" {
		key := spec.ReferenceKey(current.Ref, current.SourceLocation)
		if seen[key] {
			return current, allDiags
		}
		seen[key] = true
		var resolved *Parameter
		if spec.localRefs != nil && spec.localRefs.shouldResolve(current.Ref, current.SourceLocation) {
			value, diags := spec.localRefs.resolveTyped("parameter", current.Ref, current.SourceLocation, spec.localRefs.convertParameter)
			allDiags = append(allDiags, diags...)
			candidate, ok := value.(*Parameter)
			if ok {
				resolved = candidate
			}
		} else if spec.Components != nil {
			resolved = spec.Components.Parameters[referenceBaseName(current.Ref)]
		}
		if resolved == nil || resolved == current {
			return current, allDiags
		}
		current = resolved
	}
	return current, allDiags
}

// ResolveRequestBodyReference resolves a Request Body Reference Object.
func (spec *Spec) ResolveRequestBodyReference(body *RequestBody) (*RequestBody, []Diagnostic) {
	if body == nil || body.Ref == "" || spec == nil {
		return body, nil
	}
	current := body
	seen := make(map[string]bool)
	var allDiags []Diagnostic
	for current.Ref != "" {
		key := spec.ReferenceKey(current.Ref, current.SourceLocation)
		if seen[key] {
			return current, allDiags
		}
		seen[key] = true
		var resolved *RequestBody
		if spec.localRefs != nil && spec.localRefs.shouldResolve(current.Ref, current.SourceLocation) {
			value, diags := spec.localRefs.resolveTyped("requestBody", current.Ref, current.SourceLocation, spec.localRefs.convertRequestBody)
			allDiags = append(allDiags, diags...)
			candidate, ok := value.(*RequestBody)
			if ok {
				resolved = candidate
			}
		} else if spec.Components != nil {
			resolved = spec.Components.RequestBodies[referenceBaseName(current.Ref)]
		}
		if resolved == nil || resolved == current {
			return current, allDiags
		}
		current = resolved
	}
	return current, allDiags
}

// ResolveResponseReference resolves a Response Reference Object.
func (spec *Spec) ResolveResponseReference(response *Response) (*Response, []Diagnostic) {
	if response == nil || response.Ref == "" || spec == nil {
		return response, nil
	}
	current := response
	seen := make(map[string]bool)
	var allDiags []Diagnostic
	for current.Ref != "" {
		key := spec.ReferenceKey(current.Ref, current.SourceLocation)
		if seen[key] {
			return current, allDiags
		}
		seen[key] = true
		var resolved *Response
		if spec.localRefs != nil && spec.localRefs.shouldResolve(current.Ref, current.SourceLocation) {
			value, diags := spec.localRefs.resolveTyped("response", current.Ref, current.SourceLocation, spec.localRefs.convertResponse)
			allDiags = append(allDiags, diags...)
			candidate, ok := value.(*Response)
			if ok {
				resolved = candidate
			}
		} else if spec.Components != nil {
			resolved = spec.Components.Responses[referenceBaseName(current.Ref)]
		}
		if resolved == nil || resolved == current {
			return current, allDiags
		}
		current = resolved
	}
	return current, allDiags
}

// ResolvePathItemReference resolves a Path Item Reference Object.
func (spec *Spec) ResolvePathItemReference(item *PathItem) (*PathItem, []Diagnostic) {
	if item == nil || item.Ref == "" || spec == nil || spec.localRefs == nil {
		return item, nil
	}
	current := item
	seen := make(map[string]bool)
	var allDiags []Diagnostic
	for current.Ref != "" && spec.localRefs.shouldResolve(current.Ref, current.SourceLocation) {
		key := spec.ReferenceKey(current.Ref, current.SourceLocation)
		if seen[key] {
			return current, allDiags
		}
		seen[key] = true
		value, diags := spec.localRefs.resolveTyped("pathItem", current.Ref, current.SourceLocation, spec.localRefs.convertPathItem)
		allDiags = append(allDiags, diags...)
		resolved, ok := value.(*PathItem)
		if !ok || resolved == nil || resolved == current {
			return current, allDiags
		}
		current = resolved
	}
	return current, allDiags
}

// ResolveSecuritySchemeReference resolves a Security Scheme Reference Object.
func (spec *Spec) ResolveSecuritySchemeReference(scheme *SecurityScheme) (*SecurityScheme, []Diagnostic) {
	if scheme == nil || scheme.Ref == "" || spec == nil {
		return scheme, nil
	}
	current := scheme
	seen := make(map[string]bool)
	var allDiags []Diagnostic
	for current.Ref != "" {
		key := spec.ReferenceKey(current.Ref, current.SourceLocation)
		if seen[key] {
			return current, allDiags
		}
		seen[key] = true
		var resolved *SecurityScheme
		if spec.localRefs != nil && spec.localRefs.shouldResolve(current.Ref, current.SourceLocation) {
			value, diags := spec.localRefs.resolveTyped("securityScheme", current.Ref, current.SourceLocation, spec.localRefs.convertSecurityScheme)
			allDiags = append(allDiags, diags...)
			candidate, ok := value.(*SecurityScheme)
			if ok {
				resolved = candidate
			}
		} else if spec.Components != nil {
			resolved = spec.Components.SecuritySchemes[referenceBaseName(current.Ref)]
		}
		if resolved == nil || resolved == current {
			return current, allDiags
		}
		current = resolved
	}
	return current, allDiags
}

func (r *localRefResolver) resolveTyped(kind, ref string, loc SourceLocation, convert func(Node) (any, []Diagnostic)) (any, []Diagnostic) {
	node, key, _ := r.resolve(ref, loc)
	if node == nil {
		return nil, nil // parser.Validate owns reference-resolution diagnostics.
	}
	cacheKey := kind + "\x00" + key
	if cached, ok := r.typed[cacheKey]; ok {
		return cached.value, nil
	}
	value, diags := convert(node)
	r.typed[cacheKey] = resolvedReference{value: value}
	return value, diags
}

func (r *localRefResolver) shouldResolve(ref string, loc SourceLocation) bool {
	return !strings.HasPrefix(ref, "#") || r.documentPath(loc.File) != r.entryPath
}

func (r *localRefResolver) resolve(ref string, loc SourceLocation) (Node, string, []Diagnostic) {
	u, err := url.Parse(ref)
	if err != nil {
		return nil, ref, referenceDiagnostic(loc, "Invalid $ref", fmt.Sprintf("Could not parse $ref %q: %v.", ref, err))
	}
	if u.Scheme != "" || u.Host != "" {
		return nil, ref, referenceDiagnostic(loc, "Unsupported remote $ref", fmt.Sprintf("Remote $ref %q is not supported; use a local relative file reference.", ref))
	}
	if u.RawQuery != "" {
		return nil, ref, referenceDiagnostic(loc, "Invalid $ref", fmt.Sprintf("$ref %q must not contain a query string.", ref))
	}

	current := r.documentPath(loc.File)
	targetPath := current
	if u.Path != "" {
		if filepath.IsAbs(filepath.FromSlash(u.Path)) {
			return nil, ref, referenceDiagnostic(loc, "Unsupported absolute $ref", fmt.Sprintf("$ref %q must use a path relative to the document containing it.", ref))
		}
		targetPath, err = canonicalReferencePath(filepath.Join(filepath.Dir(current), filepath.FromSlash(u.Path)))
		if err != nil {
			return nil, ref, referenceDiagnostic(loc, "Unresolvable $ref", fmt.Sprintf("Could not resolve file path in $ref %q: %v.", ref, err))
		}
	}
	pointer := "#" + u.Fragment
	key := targetPath + pointer
	root, diags := r.loadDocument(targetPath, loc)
	if root == nil {
		return nil, key, diags
	}
	target, pointerDiags := ResolveLocalRef(root, pointer, loc)
	return target, key, pointerDiags
}

func (r *localRefResolver) documentPath(file string) string {
	if canonical := r.aliases[file]; canonical != "" {
		return canonical
	}
	if canonical, err := canonicalReferencePath(file); err == nil {
		if _, ok := r.documents[canonical]; ok {
			return canonical
		}
	}
	return r.entryPath
}

func (r *localRefResolver) loadDocument(path string, loc SourceLocation) (Node, []Diagnostic) {
	if root := r.documents[path]; root != nil {
		return root, nil
	}
	if len(r.documents) >= maxReferenceDocuments {
		return nil, referenceDiagnostic(loc, "Reference document limit exceeded", fmt.Sprintf("Local references may load at most %d documents.", maxReferenceDocuments))
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, referenceDiagnostic(loc, "Unresolvable $ref", fmt.Sprintf("Could not read referenced file %q: %v.", path, err))
	}
	if info.IsDir() {
		return nil, referenceDiagnostic(loc, "Unresolvable $ref", fmt.Sprintf("Referenced path %q is a directory, not a document.", path))
	}
	if info.Size() > maxReferenceBytes-r.totalBytes {
		return nil, referenceDiagnostic(loc, "Reference byte limit exceeded", fmt.Sprintf("Local reference documents may total at most %d bytes.", maxReferenceBytes))
	}
	//nolint:gosec // A local entry spec explicitly authorizes its relative sibling references.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, referenceDiagnostic(loc, "Unresolvable $ref", fmt.Sprintf("Could not read referenced file %q: %v.", path, err))
	}
	root, err := LoadFile(path, data)
	if err != nil {
		return nil, referenceDiagnostic(loc, "Invalid referenced document", fmt.Sprintf("Could not parse referenced file %q: %v.", path, err))
	}
	r.documents[path] = root
	r.aliases[root.GetSourceLocation().File] = path
	r.totalBytes += int64(len(data))
	return root, nil
}

func (r *localRefResolver) convertSchema(node Node) (any, []Diagnostic) {
	if r.version == Version2_0 {
		return parseSchema(node)
	}
	c := r.converter()
	return c.convertSchema(node), c.diags
}

func (r *localRefResolver) convertParameter(node Node) (any, []Diagnostic) {
	var parameter *Parameter
	var diags []Diagnostic
	if r.version == Version2_0 {
		parsed, parseDiags := parseParameter(node)
		parameter, diags = &parsed, parseDiags
	} else {
		c := r.converter()
		parameter, diags = c.convertParameter(node), c.diags
	}
	validator := nestedRequiredValidator{version: r.version}
	validator.checkParameter(parameter, "referenced parameter")
	return parameter, append(diags, validator.diags...)
}

func (r *localRefResolver) convertRequestBody(node Node) (any, []Diagnostic) {
	c := r.converter()
	return c.convertRequestBody(node), c.diags
}

func (r *localRefResolver) convertResponse(node Node) (any, []Diagnostic) {
	var response *Response
	var diags []Diagnostic
	if r.version == Version2_0 {
		response, diags = parseResponse(node, r.produces)
	} else {
		c := r.converter()
		response, diags = c.convertResponse(node), c.diags
	}
	validator := nestedRequiredValidator{version: r.version}
	validator.checkResponse(response, "referenced response")
	return response, append(diags, validator.diags...)
}

func (r *localRefResolver) convertPathItem(node Node) (any, []Diagnostic) {
	var item *PathItem
	var diags []Diagnostic
	if r.version == Version2_0 {
		mapping, ok := node.(*MapNode)
		if !ok {
			return nil, []Diagnostic{diagInvalidType("path item", "object", node)}
		}
		item, diags = parsePathItem(mapping, r.produces, r.consumes, r.schemes)
	} else {
		c := r.converter()
		item, diags = c.convertPathItem(node), c.diags
	}
	validator := nestedRequiredValidator{version: r.version}
	validator.checkPathItem(item, "referenced path item")
	return item, append(diags, validator.diags...)
}

func (r *localRefResolver) convertSecurityScheme(node Node) (any, []Diagnostic) {
	if r.version == Version2_0 {
		return parseSecurityScheme(node)
	}
	c := r.converter()
	return c.convertSecurityScheme(node), c.diags
}

func (r *localRefResolver) converter() *v30Converter {
	return &v30Converter{version: r.version, budget: NewBudget(DefaultLimits())}
}

func canonicalReferencePath(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if resolved, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		return resolved, nil
	}
	return abs, nil
}

func referenceBaseName(ref string) string {
	if index := strings.LastIndex(ref, "/"); index >= 0 {
		ref = ref[index+1:]
	}
	ref = strings.ReplaceAll(ref, "~1", "/")
	return strings.ReplaceAll(ref, "~0", "~")
}

func referenceDiagnostic(loc SourceLocation, summary, detail string) []Diagnostic {
	return []Diagnostic{{
		Severity:       SeverityError,
		Summary:        summary,
		Detail:         detail,
		SourceLocation: &loc,
	}}
}
