// Package transformer maps normalized OpenAPI schemas to Terraform Plugin Framework
// representations used by the Eidos provider generator.
package transformer

import (
	"regexp"
	"sort"
	"strings"
)

// HTTPMethod is a normalized HTTP verb used by CRUD inference.
type HTTPMethod string

// HTTP method constants.
const (
	// MethodGet is the HTTP GET method.
	MethodGet HTTPMethod = "GET"
	// MethodPost is the HTTP POST method.
	MethodPost HTTPMethod = "POST"
	// MethodPut is the HTTP PUT method.
	MethodPut HTTPMethod = "PUT"
	// MethodPatch is the HTTP PATCH method.
	MethodPatch HTTPMethod = "PATCH"
	// MethodDelete is the HTTP DELETE method.
	MethodDelete HTTPMethod = "DELETE"
)

// Operation is a normalized OpenAPI path operation. Only the fields needed for
// CRUD and ID inference are retained; richer request/response metadata is left
// to later transformation stages.
type Operation struct {
	Method        HTTPMethod
	Path          string
	OperationID   string
	Parameters    []Parameter
	RequestBody   bool
	ResponseBody  bool
	RequestSchema *SchemaSpec
	// RequestMediaType is the request body's selected media type
	// ("application/json", "application/x-www-form-urlencoded",
	// "multipart/form-data", "application/xml", ...), chosen deterministically
	// (application/json preferred, else the first schema-bearing media type
	// lexicographically) from the request body's content map. Empty for bodiless
	// operations. Carried onto OperationMappingIR.MediaType so the generator emits
	// the matching body encoding (A2).
	RequestMediaType string
	ResponseSchema   *SchemaSpec
	// ResponseEnvelope is the property name of a {data: ...} response envelope
	// that OperationsFromSpecWithDiagnostics unwrapped from ResponseSchema (e.g.
	// "data" for a response shaped {"data": <payload>}). It is empty when the
	// response is not enveloped. The generator reads it to unwrap the decoded
	// response body before applying it to the model, so the schema and the
	// response stay consistent after the envelope is flattened.
	ResponseEnvelope string
	// ResponseHeaders holds response header names used for pagination/style detection.
	// It is populated from the successful response's headers by OperationsFromSpec.
	ResponseHeaders []string
	// Extensions holds OpenAPI extensions such as x-pagination.
	// It is populated from the parsed operation's extensions by OperationsFromSpec.
	Extensions map[string]any
}

// Parameter is a normalized OpenAPI operation parameter.
type Parameter struct {
	Name     string
	In       string // path, query, header, cookie
	Required bool
	Type     string
	// ItemsType is the scalar element type when Type is "array" (the `items`
	// type of an array parameter); empty for non-array parameters. Used to model
	// an array query parameter as a List of the element primitive so the
	// generator serializes one repeated query value per element.
	ItemsType string
	// Style is the OpenAPI 3.x serialization style of the parameter (e.g.
	// "form", "spaceDelimited", "pipeDelimited"); the v2 parser converts
	// collectionFormat to style. Empty means the default ("form" for query). The
	// array-query-parameter modeling serializes repeated values (form +
	// explode: true); a non-form style is lossy and surfaced with a fail-loud
	// warning rather than dropped silently.
	Style string
}

// IDKind classifies how a resource instance is identified.
type IDKind string

const (
	// IDSimple means the resource is identified by a single path parameter.
	IDSimple IDKind = "simple"
	// IDComposite means the resource is identified by multiple path parameters,
	// typically nested under a parent collection.
	IDComposite IDKind = "composite"
)

// IDInfo describes the identifier inferred from an instance path template.
type IDInfo struct {
	Kind           IDKind
	ParameterNames []string
	// AttributeName is the Terraform-style snake_case name of the id attribute
	// for simple IDs (e.g. "pet_id").
	AttributeName string
	// ImportFormat is a printf-style format used to parse composite import IDs.
	// For a simple ID it is "%s"; for two parameters it is "%s:%s", etc.
	ImportFormat string
}

// ResourceCRUD records the operations inferred for a single Terraform managed
// resource. At most one Update operation is preferred: PUT wins over PATCH
// when both are present, matching PROJECT_DESIGN.md Section 8.2.
type ResourceCRUD struct {
	Name           string
	CollectionPath string
	InstancePath   string
	Create         *Operation
	Read           *Operation
	Update         *Operation // preferred update operation
	FullUpdate     *Operation // PUT, if present
	PartialUpdate  *Operation // PATCH, if present
	Delete         *Operation
	List           *Operation
	ID             IDInfo
}

// pathSegment is a single path component, either a static literal or a
// templated path parameter.
type pathSegment struct {
	Value   string
	IsParam bool
}

// pathKey is an internal grouping key that preserves the distinction between
// static and parameter segments.
type pathKey string

var pathParamRe = regexp.MustCompile(`^\{([^}:]+)(?::[^}]*)?\}$`)

// InferResourceCRUD analyzes a normalized set of OpenAPI paths and operations
// and infers Terraform managed resources with their CRUD mappings.
//
// When usePutAsCreate is true (the default for auto-generated providers), a
// CRUD group whose collection path has no POST but whose instance path has a
// PUT (plus GET/DELETE) uses that PUT as the resource's Create mapping — an
// upsert. The same PUT remains the Update mapping; Create and Update both issue
// the upsert, which is correct. Collection POST still wins when present. This
// surfaces an upsert-capable resource that would otherwise stay permanently
// scaffolded; the handler emits an Info diagnostic when it fires so the
// inference is never silent (AGENTS.md "fail loud, never silently").
//
// The returned resources are sorted deterministically by resource name.
func InferResourceCRUD(pathOps map[string]map[HTTPMethod]Operation, usePutAsCreate bool) []ResourceCRUD {
	parsed := make(map[string][]pathSegment, len(pathOps))
	prefixKeys := make(map[string]pathKey, len(pathOps))
	pathKeys := make(map[string]pathKey, len(pathOps))

	// Iterate paths in sorted order so the grouping and instance-path selection
	// are deterministic regardless of map iteration seed (M-39).
	for _, path := range sortedKeys(pathOps) {
		segs := parsePath(path)
		parsed[path] = segs
		pathKeys[path] = keyForSegments(segs)
		prefixKeys[path] = keyForSegments(prefixSegments(segs))
	}

	groups := make(map[pathKey][]string)
	for _, path := range sortedKeys(pathOps) {
		groups[prefixKeys[path]] = append(groups[prefixKeys[path]], path)
	}

	resources := make([]ResourceCRUD, 0, len(groups))
	groupKeys := make([]pathKey, 0, len(groups))
	for pk := range groups {
		groupKeys = append(groupKeys, pk)
	}
	sort.Slice(groupKeys, func(i, j int) bool { return groupKeys[i] < groupKeys[j] })
	for _, pk := range groupKeys {
		paths := groups[pk]
		resources = append(resources, buildResourceCRUD(pk, paths, pathOps, parsed, pathKeys, usePutAsCreate))
	}

	// Stable sort over already-deterministic input yields fully deterministic
	// output, and dedupCRUDByName collapses same-named resources (e.g. /v1/pets
	// and /v2/pets) so they cannot collide downstream (M-39). When two groups
	// share a name, the one with the more complete CRUD wins — otherwise a
	// degenerate same-named sub-path group (e.g. RBAC /access-control/.../teams)
	// could shadow a full-CRUD group (e.g. /teams) and silently drop it (G7).
	sort.SliceStable(resources, func(i, j int) bool {
		return resources[i].Name < resources[j].Name
	})
	resources = dedupCRUDByName(resources)
	return resources
}

// HasFullCRUD reports whether the operation at (path, method) belongs to a
// complete CRUD group (Create + Read + Delete). Operations that are not part of
// a full CRUD group are reclassified as actions by the API layer: a scaffolded
// resource with an empty model is worse than a wired action, and a resource
// without a Delete cannot be destroyed by Terraform.
func HasFullCRUD(path string, method HTTPMethod, pathOps map[string]map[HTTPMethod]Operation) bool {
	for _, g := range InferResourceCRUD(pathOps, true) {
		if g.Create == nil || g.Read == nil || g.Delete == nil {
			continue
		}
		if groupHasOperation(g, path, method) {
			return true
		}
	}
	return false
}

// groupHasOperation reports whether the CRUD group contains the given
// operation, comparing both path and method.
func groupHasOperation(g ResourceCRUD, path string, method HTTPMethod) bool {
	for _, op := range []*Operation{g.Create, g.Read, g.Update, g.Delete} {
		if op != nil && op.Path == path && op.Method == method {
			return true
		}
	}
	return false
}

// crudCompleteness returns a count of the CRUD operations a group actually
// wires, used to prefer a fully-managed group over a same-named degenerate one
// during dedup.
func crudCompleteness(r ResourceCRUD) int {
	n := 0
	for _, op := range []*Operation{r.Create, r.Read, r.Update, r.Delete, r.List} {
		if op != nil {
			n++
		}
	}
	return n
}

// dedupCRUDByName keeps the first group for each name, but when a name collides
// the group with the higher CRUD-completeness score wins (stable for ties).
func dedupCRUDByName(items []ResourceCRUD) []ResourceCRUD {
	if len(items) == 0 {
		return items
	}
	best := make(map[string]int, len(items))
	out := items[:0]
	for _, it := range items {
		score := crudCompleteness(it)
		if prev, ok := best[it.Name]; ok {
			if prev >= score {
				continue // the earlier, equally-or-more-complete group wins
			}
			// The later group is more complete; replace the earlier entry.
			best[it.Name] = score
			for i := range out {
				if out[i].Name == it.Name {
					out[i] = it
					break
				}
			}
			continue
		}
		best[it.Name] = score
		out = append(out, it)
	}
	return out
}

func buildResourceCRUD(prefixKey pathKey, paths []string, pathOps map[string]map[HTTPMethod]Operation, parsed map[string][]pathSegment, pathKeys map[string]pathKey, usePutAsCreate bool) ResourceCRUD {
	// The collection path is the prefix itself. If the prefix is not present
	// as an original path, reconstruct it from the key.
	collectionPath := string(prefixKey)
	for _, p := range paths {
		if pathKeys[p] == prefixKey {
			collectionPath = p
			break
		}
	}

	// Choose the deepest path in the group that contains path parameters and
	// is not the collection path itself as the instance path.
	var instancePath string
	var instanceSegs []pathSegment
	for _, p := range paths {
		if pathKeys[p] == prefixKey {
			continue
		}
		segs := parsed[p]
		if !hasAnyParam(segs) {
			continue
		}
		if instancePath == "" || len(segs) > len(instanceSegs) {
			instancePath = p
			instanceSegs = segs
		}
	}

	resource := ResourceCRUD{
		Name:           resourceNameFromPath(collectionPath),
		CollectionPath: collectionPath,
		InstancePath:   instancePath,
	}

	if ops, ok := pathOps[collectionPath]; ok {
		resource.Create = cloneOp(ops, MethodPost)
		resource.List = cloneOp(ops, MethodGet)
	}

	if instancePath != "" {
		resource.ID = detectID(instanceSegs)
		if ops, ok := pathOps[instancePath]; ok {
			resource.Read = cloneOp(ops, MethodGet)
			resource.Delete = cloneOp(ops, MethodDelete)
			resource.Update, resource.FullUpdate, resource.PartialUpdate = chooseUpdateOps(ops)
			// PUT-as-create (upsert): when the collection path has no POST but
			// the instance path has a PUT, use it as the Create mapping. The same
			// PUT already became Update above; Create and Update both issue the
			// upsert, which is correct. Collection POST still wins (Create was set
			// above from the collection path in that case). Gated on usePutAsCreate
			// so the kill-switch (use_put_as_create: false) restores the legacy
			// scaffold behavior where the group stays Create-less.
			if resource.Create == nil && usePutAsCreate {
				if put := cloneOp(ops, MethodPut); put != nil {
					resource.Create = put
				}
			}
		}
	}

	return resource
}

func chooseUpdateOps(ops map[HTTPMethod]Operation) (update, fullUpdate, partialUpdate *Operation) {
	put := cloneOp(ops, MethodPut)
	patch := cloneOp(ops, MethodPatch)
	if put != nil && patch != nil {
		return put, put, patch
	}
	if put != nil {
		return put, put, nil
	}
	if patch != nil {
		return patch, nil, patch
	}
	return nil, nil, nil
}

func parsePath(path string) []pathSegment {
	parts := strings.Split(path, "/")
	segs := make([]pathSegment, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		if m := pathParamRe.FindStringSubmatch(p); m != nil {
			segs = append(segs, pathSegment{Value: m[1], IsParam: true})
		} else {
			segs = append(segs, pathSegment{Value: p, IsParam: false})
		}
	}
	return segs
}

func keyForSegments(segs []pathSegment) pathKey {
	parts := make([]string, len(segs))
	for i, s := range segs {
		if s.IsParam {
			parts[i] = "{" + s.Value + "}"
		} else {
			parts[i] = s.Value
		}
	}
	return pathKey("/" + strings.Join(parts, "/"))
}

func prefixSegments(segs []pathSegment) []pathSegment {
	n := len(segs)
	for n > 0 && segs[n-1].IsParam {
		n--
	}
	return segs[:n]
}

func hasAnyParam(segs []pathSegment) bool {
	for _, s := range segs {
		if s.IsParam {
			return true
		}
	}
	return false
}

func resourceNameFromPath(path string) string {
	segs := parsePath(path)
	for i := len(segs) - 1; i >= 0; i-- {
		if !segs[i].IsParam {
			return singularize(segs[i].Value)
		}
	}
	return "resource"
}

func detectID(segs []pathSegment) IDInfo {
	var params []string
	for _, s := range segs {
		if s.IsParam {
			params = append(params, s.Value)
		}
	}

	info := IDInfo{
		Kind:           IDSimple,
		ParameterNames: params,
		ImportFormat:   "%s",
	}
	if len(params) == 0 {
		return info
	}
	if len(params) > 1 {
		info.Kind = IDComposite
		info.ImportFormat = strings.Repeat("%s:", len(params))
		info.ImportFormat = info.ImportFormat[:len(info.ImportFormat)-1]
	} else {
		info.AttributeName = ToSnakeCase(params[0])
	}
	return info
}

// cloneOp returns an isolated copy of the operation for method so that CRUD
// inference can mutate Parameters/ResponseHeaders/Extensions without corrupting
// the caller's pathOps. It reuses cloneOperation (the deep clone datasource.go
// relies on) rather than aliasing the map value's slices (L-93).
func cloneOp(ops map[HTTPMethod]Operation, method HTTPMethod) *Operation {
	if op, ok := ops[method]; ok {
		return cloneOperation(&op)
	}
	return nil
}

func singularize(s string) string {
	switch s {
	case "species", "series":
		return s
	}
	if strings.HasSuffix(s, "ies") && len(s) > 3 && !isVowel(s[len(s)-4]) {
		return s[:len(s)-3] + "y"
	}
	if strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") && len(s) > 1 {
		return s[:len(s)-1]
	}
	return s
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}
