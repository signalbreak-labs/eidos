package schema

// JSONConvertTemplate is the static template for the generated
// internal/provider/json_convert.go file: generic conversions between the
// Terraform Plugin Framework model structs and JSON-ready maps. Its content
// does not depend on the provider IR, so it is a plain text constant.
const JSONConvertTemplate = `package provider

import (
{{ if .IncludeXML }}	"bytes"
	"encoding/xml"
{{ end }}	"context"
	"encoding/json"
	"fmt"
	"reflect"
{{ if .IncludeXML }}	"sort"
{{ end }}
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// wireNames maps each generated model struct's nested attribute paths to the
// API's original property names when they differ from the snake_case Terraform
// attribute names (G18). Paths are dot-joined tfsdk attribute names; collection
// elements are denoted with a "*" segment (e.g. "modules.*.symbol"). The map is
// consulted by modelToJSONMap and applyJSONToModel so request bodies and
// response mapping use the API's wire names for nested objects.
var wireNames = map[string]map[string]string{
{{ .WireNamesBody }}
}

// modelToJSONMap converts a generated Terraform model struct into a JSON-ready
// map keyed by tfsdk attribute name. Null and unknown attribute values are
// omitted so optional attributes are not sent to the API. Dynamic attribute
// values cannot be represented as JSON and produce an error instead of being
// silently dropped.
func modelToJSONMap(model any) (map[string]any, error) {
	v := reflect.ValueOf(model)
	// Strip every layer of indirection so callers may pass a value, a pointer,
	// or a pointer-to-pointer (the extracted *Remote helpers receive the model
	// as a pointer parameter and the framework glue addresses it once more).
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct model, got %s", v.Kind())
	}
	t := v.Type()
	wireMap := wireNames[t.Name()]
	out := make(map[string]any, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Tag.Get("tfsdk")
		if name == "" {
			continue
		}
		// The json tag carries the API's original property name when it differs
		// from the snake_case Terraform attribute name (G18).
		wire := t.Field(i).Tag.Get("json")
		if wire == "" {
			wire = name
		}
		value, ok := v.Field(i).Interface().(attr.Value)
		if !ok {
			continue
		}
		converted, keep, err := attrValueToJSON(value, wireMap, name)
		if err != nil {
			return nil, fmt.Errorf("attribute %q: %w", name, err)
		}
		if keep {
			{{ if .IncludeWirePath -}}
			putJSONValue(out, t.Field(i).Tag.Get("jsonpath"), wire, converted)
			{{ else -}}
			out[wire] = converted
			{{ end -}}
		}
	}
	return out, nil
}

{{ if .IncludeWirePath -}}
// putJSONValue writes an ordinary top-level API field or a managed-resource
// path parameter promoted from one nested object. Map values are merged so a
// retained wrapper attribute and its promoted identity fields can share the
// same wire object regardless of generated struct-field order.
func putJSONValue(out map[string]any, parent, name string, value any) {
	if parent == "" {
		if incoming, ok := value.(map[string]any); ok {
			if existing, ok := out[name].(map[string]any); ok {
				for key, nested := range incoming {
					existing[key] = nested
				}
				return
			}
		}
		out[name] = value
		return
	}
	nested, ok := out[parent].(map[string]any)
	if !ok {
		nested = make(map[string]any)
		out[parent] = nested
	}
	nested[name] = value
}
{{ end -}}

// applyJSONToModel updates the fields of a generated Terraform model struct
// from a decoded JSON object keyed by tfsdk attribute name. Keys absent from
// data leave the corresponding fields untouched, so attributes the API does
// not return keep their current values.
func applyJSONToModel(model any, data map[string]any) error {
	v := reflect.ValueOf(model)
	// Strip every layer of indirection; see modelToJSONMap for the rationale.
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("expected a struct model, got %s", v.Kind())
	}
	t := v.Type()
	wireMap := wireNames[t.Name()]
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Tag.Get("tfsdk")
		if name == "" {
			continue
		}
		// The json tag carries the API's original property name when it differs
		// from the snake_case Terraform attribute name (G18).
		wire := t.Field(i).Tag.Get("json")
		if wire == "" {
			wire = name
		}
		{{ if .IncludeWirePath -}}
		raw, ok, err := jsonFieldValue(data, t.Field(i).Tag.Get("jsonpath"), name, wire)
		if err != nil {
			return fmt.Errorf("attribute %q: %w", name, err)
		}
		{{ else -}}
		raw, ok := data[name]
		if !ok {
			raw, ok = data[wire]
		}
		{{ end -}}
		if !ok {
			continue
		}
		field := v.Field(i)
		current, ok := field.Interface().(attr.Value)
		if !ok {
			continue
		}
		converted, err := jsonToAttrValue(current, raw, wireMap, name)
		if err != nil {
			return fmt.Errorf("attribute %q: %w", name, err)
		}
		field.Set(reflect.ValueOf(converted))
	}
	// Null-default any attribute the response did not carry so the resulting
	// state is fully KNOWN: the framework rejects unknown values after apply,
	// and a computed attribute the API never returns must become null rather
	// than staying unknown (G18).
	for i := 0; i < t.NumField(); i++ {
		field := v.Field(i)
		current, ok := field.Interface().(attr.Value)
		if !ok || !current.IsUnknown() {
			continue
		}
		ctx := context.Background()
		at := current.Type(ctx)
		nullVal, err := at.ValueFromTerraform(ctx, tftypes.NewValue(at.TerraformType(ctx), nil))
		if err != nil {
			return fmt.Errorf("attribute %q: %w", t.Field(i).Tag.Get("tfsdk"), err)
		}
		field.Set(reflect.ValueOf(nullVal))
	}
	return nil
}

{{ if .IncludeWirePath -}}
// jsonFieldValue reads an ordinary top-level API field or unwraps a field that
// the managed-resource schema promoted from one nested object. A top-level
// fallback keeps generated coverage fixtures and APIs that return either shape
// compatible while nested wire data remains authoritative when present.
func jsonFieldValue(data map[string]any, parent, name, wire string) (any, bool, error) {
	if parent != "" {
		if parentValue, exists := data[parent]; exists {
			nested, ok := parentValue.(map[string]any)
			if !ok {
				return nil, false, fmt.Errorf("expected wire parent %q to be an object", parent)
			}
			if raw, ok := nested[name]; ok {
				return raw, true, nil
			}
			if raw, ok := nested[wire]; ok {
				return raw, true, nil
			}
		}
	}
	if raw, ok := data[name]; ok {
		return raw, true, nil
	}
	raw, ok := data[wire]
	return raw, ok, nil
}
{{ end -}}

// attrValueToJSON converts a framework attribute value to a JSON-ready Go
// value. The boolean result reports whether the value should be included in
// the output; null and unknown values are excluded. wireMap carries the model's
// nested wire names and path is the dot-joined tfsdk path of the value, so
// nested object attributes are emitted under their API property names (G18).
func attrValueToJSON(value attr.Value, wireMap map[string]string, path string) (any, bool, error) {
	if value == nil || value.IsNull() || value.IsUnknown() {
		return nil, false, nil
	}
	switch v := value.(type) {
	case types.String:
		return v.ValueString(), true, nil
	case types.Int64:
		return v.ValueInt64(), true, nil
	case types.Float64:
		return v.ValueFloat64(), true, nil
	case types.Bool:
		return v.ValueBool(), true, nil
	case types.Number:
		// A number inside a Dynamic attribute is parsed by the framework as a
		// basetypes.NumberValue (arbitrary precision), not Int64/Float64, so it
		// is not matched by the cases above. Render it as a json.Number to
		// preserve precision and emit a raw JSON number (no quotes) in the
		// request body.
		return json.Number(v.ValueBigFloat().Text('f', -1)), true, nil
	case types.List:
		return elementsToJSON(v.Elements(), wireMap, path+".*")
	case types.Set:
		return elementsToJSON(v.Elements(), wireMap, path+".*")
	case types.Tuple:
		// A Dynamic attribute that the practitioner configures with a list
		// literal (e.g. "allowed_to_merge = [ null ]" or a heterogeneous array)
		// is parsed by the framework as a Tuple value, not a List: a Dynamic
		// attribute's concrete element type is decided per value, and a list
		// literal whose elements are null or of differing types cannot be a
		// homogeneous List, so it becomes a Tuple. Serialize it as a JSON array
		// of its elements so the request body carries the array the API expects
		// (G18). Without this case the request-body builder returns "unsupported
		// attribute value type basetypes.TupleValue" and every Create/Update on
		// such a resource fails.
		return elementsToJSON(v.Elements(), wireMap, path+".*")
	case types.Map:
		return mapToJSON(v.Elements(), wireMap, path+".*")
	case types.Object:
		return stringMapToJSON(v.Attributes(), wireMap, path)
	case types.Dynamic:
		// A dynamic attribute wraps an arbitrary attr.Value; convert the
		// wrapped value so the request body carries the practitioner's dynamic
		// payload (G18).
		return attrValueToJSON(v.UnderlyingValue(), wireMap, path)
	}
	return nil, false, fmt.Errorf("unsupported attribute value type %T", value)
}

func elementsToJSON(elements []attr.Value, wireMap map[string]string, path string) (any, bool, error) {
	out := make([]any, 0, len(elements))
	for _, element := range elements {
		converted, keep, err := attrValueToJSON(element, wireMap, path)
		if err != nil {
			return nil, false, err
		}
		if !keep {
			converted = nil
		}
		out = append(out, converted)
	}
	return out, true, nil
}

// stringMapToJSON converts an object's attributes to a JSON object keyed by
// wire name. The wire map is consulted per attribute so nested objects use the
// API's property names (G18).
func stringMapToJSON(values map[string]attr.Value, wireMap map[string]string, path string) (any, bool, error) {
	out := make(map[string]any, len(values))
	for name, value := range values {
		wire := name
		if wireMap != nil {
			if w, ok := wireMap[path+"."+name]; ok {
				wire = w
			}
		}
		converted, keep, err := attrValueToJSON(value, wireMap, path+"."+name)
		if err != nil {
			return nil, false, err
		}
		if !keep {
			converted = nil
		}
		out[wire] = converted
	}
	return out, true, nil
}

// mapToJSON converts a map attribute to a JSON object. Map keys are arbitrary
// practitioner data, not schema attributes, so they are emitted verbatim; only
// the element values are converted (with the "*" path segment standing in for
// the map key so nested object elements resolve their wire names).
func mapToJSON(values map[string]attr.Value, wireMap map[string]string, path string) (any, bool, error) {
	out := make(map[string]any, len(values))
	for name, value := range values {
		converted, keep, err := attrValueToJSON(value, wireMap, path)
		if err != nil {
			return nil, false, err
		}
		if !keep {
			converted = nil
		}
		out[name] = converted
	}
	return out, true, nil
}

// jsonToAttrValue converts a decoded JSON value back into a framework
// attribute value with the same type as current. A nil raw value produces the
// null value for the attribute type. wireMap carries the model's nested wire
// names and path is the dot-joined tfsdk path of the value, so nested object
// attributes are read from the API's property names (G18).
func jsonToAttrValue(current attr.Value, raw any, wireMap map[string]string, path string) (attr.Value, error) {
	if raw == nil {
		return nullAttrValue(current)
	}
	switch v := current.(type) {
	case types.String:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected JSON string, got %T", raw)
		}
		return types.StringValue(s), nil
	case types.Int64:
		n, ok := raw.(json.Number)
		if !ok {
			return nil, fmt.Errorf("expected JSON number, got %T", raw)
		}
		i, err := n.Int64()
		if err != nil {
			return nil, fmt.Errorf("expected JSON integer, got %q", n.String())
		}
		return types.Int64Value(i), nil
	case types.Float64:
		n, ok := raw.(json.Number)
		if !ok {
			return nil, fmt.Errorf("expected JSON number, got %T", raw)
		}
		f, err := n.Float64()
		if err != nil {
			return nil, fmt.Errorf("expected JSON number, got %q", n.String())
		}
		return types.Float64Value(f), nil
	case types.Bool:
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("expected JSON boolean, got %T", raw)
		}
		return types.BoolValue(b), nil
	case types.List:
		elements, err := jsonToElements(v.ElementType(context.Background()), raw, wireMap, path+".*")
		if err != nil {
			return nil, err
		}
		list, diags := types.ListValue(v.ElementType(context.Background()), elements)
		if diags.HasError() {
			return nil, fmt.Errorf("%s", diags.Errors()[0].Detail())
		}
		return list, nil
	case types.Set:
		elements, err := jsonToElements(v.ElementType(context.Background()), raw, wireMap, path+".*")
		if err != nil {
			return nil, err
		}
		set, diags := types.SetValue(v.ElementType(context.Background()), elements)
		if diags.HasError() {
			return nil, fmt.Errorf("%s", diags.Errors()[0].Detail())
		}
		return set, nil
	case types.Map:
		values, err := jsonToStringMap(v.ElementType(context.Background()), raw, wireMap, path+".*")
		if err != nil {
			return nil, err
		}
		m, diags := types.MapValue(v.ElementType(context.Background()), values)
		if diags.HasError() {
			return nil, fmt.Errorf("%s", diags.Errors()[0].Detail())
		}
		return m, nil
	case types.Object:
		attrTypes := v.AttributeTypes(context.Background())
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected JSON object, got %T", raw)
		}
		values := make(map[string]attr.Value, len(attrTypes))
		for name, attrType := range attrTypes {
			wire := name
			if wireMap != nil {
				if w, ok := wireMap[path+"."+name]; ok {
					wire = w
				}
			}
			raw, ok := obj[wire]
			if !ok && wire != name {
				raw, ok = obj[name]
			}
			converted, err := jsonToAttrValueOfType(attrType, raw, wireMap, path+"."+name)
			if err != nil {
				return nil, fmt.Errorf("attribute %q: %w", name, err)
			}
			values[name] = converted
		}
		object, diags := types.ObjectValue(attrTypes, values)
		if diags.HasError() {
			return nil, fmt.Errorf("%s", diags.Errors()[0].Detail())
		}
		return object, nil
	case types.Dynamic:
		// A dynamic attribute carries arbitrary JSON. tftypes' DynamicPseudoType
		// parser (jsonUnmarshalDynamicPseudoType) does NOT accept raw JSON; it
		// requires an envelope {"type":<type-json>,"value":<value-json>}. Feeding
		// it a raw array, scalar, or plain object yields "invalid JSON, expected
		// '{'" (for arrays: "expected '{', got '['"), so a Dynamic attribute whose
		// API value is non-null — e.g. GitLab allowed_to_merge (an array) or
		// Grafana alert_rule data — could never be read into state. Infer the
		// concrete tftypes.Type from the decoded Go value, build the envelope, and
		// let tftypes parse the value against that concrete type (G18).
		return dynamicValueFromRaw(raw)
	}
	return nil, fmt.Errorf("unsupported attribute value type %T", current)
}

// inferTFTypes derives the concrete tftypes.Type of a decoded JSON value so a
// Dynamic attribute can be round-tripped through tftypes' DynamicPseudoType
// envelope (see dynamicValueFromRaw). Decoded numbers are json.Number when the
// decoder used UseNumber (the generated CRUD always does); float64 is accepted
// as a fallback. Arrays of a single element type become a List; arrays whose
// elements have differing types become a Tuple (the heterogeneous case that a
// Dynamic array legitimately represents). Empty arrays become a List of
// DynamicPseudoType so the element type is unconstrained.
func inferTFTypes(raw any) (tftypes.Type, error) {
	switch v := raw.(type) {
	case nil:
		// A null has no type of its own. tftypes' DynamicPseudoType parser
		// rejects raw null (it expects the {"type","value"} envelope), so a null
		// element/property must be typed as a concrete primitive for tftypes to
		// parse it as the null value of that type. String is the safe default:
		// the value is null regardless, so it round-trips as null, and String is
		// valid in both List and Tuple element positions and Object attributes.
		return tftypes.String, nil
	case bool:
		return tftypes.Bool, nil
	case json.Number:
		return tftypes.Number, nil
	case float64:
		return tftypes.Number, nil
	case string:
		return tftypes.String, nil
	case []any:
		if len(v) == 0 {
			// An empty array has no elements to infer from; use a concrete
			// element type so tftypes parses it (List{DynamicPseudoType} would
			// call TypeFromElements on no elements and fail).
			return tftypes.List{ElementType: tftypes.String}, nil
		}
		elemTypes := make([]tftypes.Type, 0, len(v))
		for _, e := range v {
			et, err := inferTFTypes(e)
			if err != nil {
				return nil, err
			}
			elemTypes = append(elemTypes, et)
		}
		first := elemTypes[0]
		homogeneous := true
		for _, et := range elemTypes[1:] {
			if !et.Is(first) {
				homogeneous = false
				break
			}
		}
		if homogeneous {
			return tftypes.List{ElementType: first}, nil
		}
		return tftypes.Tuple{ElementTypes: elemTypes}, nil
	case map[string]any:
		attrTypes := make(map[string]tftypes.Type, len(v))
		for k, val := range v {
			at, err := inferTFTypes(val)
			if err != nil {
				return nil, err
			}
			attrTypes[k] = at
		}
		return tftypes.Object{AttributeTypes: attrTypes}, nil
	default:
		return nil, fmt.Errorf("cannot infer tftypes.Type from %T", raw)
	}
}

// dynamicValueFromRaw builds a types.Dynamic from an arbitrary decoded JSON
// value. tftypes' DynamicPseudoType parser requires the
// {"type":<type-json>,"value":<value-json>} envelope, so the concrete type is
// inferred from the Go value (inferTFTypes), both halves are marshaled, and
// tftypes parses the value against that concrete type. A nil raw value yields
// the null Dynamic (G18).
func dynamicValueFromRaw(raw any) (attr.Value, error) {
	if raw == nil {
		return types.DynamicNull(), nil
	}
	typ, err := inferTFTypes(raw)
	if err != nil {
		return nil, fmt.Errorf("could not infer dynamic type: %w", err)
	}
	typeJSON, err := json.Marshal(typ)
	if err != nil {
		return nil, fmt.Errorf("could not marshal dynamic type: %w", err)
	}
	valueJSON, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("could not marshal dynamic value: %w", err)
	}
	envelope := make([]byte, 0, len(typeJSON)+len(valueJSON)+16)
	envelope = append(envelope, "{\"type\":"...)
	envelope = append(envelope, typeJSON...)
	envelope = append(envelope, ",\"value\":"...)
	envelope = append(envelope, valueJSON...)
	envelope = append(envelope, '}')
	tv, err := tftypes.ValueFromJSON(envelope, tftypes.DynamicPseudoType)
	if err != nil {
		return nil, fmt.Errorf("could not parse dynamic value: %w", err)
	}
	return types.DynamicType.ValueFromTerraform(context.Background(), tv)
}

// jsonToAttrValueOfType converts a decoded JSON value into a framework
// attribute value of the given type.
func jsonToAttrValueOfType(attrType attr.Type, raw any, wireMap map[string]string, path string) (attr.Value, error) {
	null, err := nullAttrValueOfType(attrType)
	if err != nil {
		return nil, err
	}
	return jsonToAttrValue(null, raw, wireMap, path)
}

func jsonToElements(elementType attr.Type, raw any, wireMap map[string]string, path string) ([]attr.Value, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON array, got %T", raw)
	}
	elements := make([]attr.Value, 0, len(items))
	for _, item := range items {
		converted, err := jsonToAttrValueOfType(elementType, item, wireMap, path)
		if err != nil {
			return nil, err
		}
		elements = append(elements, converted)
	}
	return elements, nil
}

func jsonToStringMap(elementType attr.Type, raw any, wireMap map[string]string, path string) (map[string]attr.Value, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON object, got %T", raw)
	}
	values := make(map[string]attr.Value, len(obj))
	for name, item := range obj {
		converted, err := jsonToAttrValueOfType(elementType, item, wireMap, path)
		if err != nil {
			return nil, err
		}
		values[name] = converted
	}
	return values, nil
}

// nullAttrValue returns the null value with the same type as current.
func nullAttrValue(current attr.Value) (attr.Value, error) {
	switch v := current.(type) {
	case types.String:
		return types.StringNull(), nil
	case types.Int64:
		return types.Int64Null(), nil
	case types.Float64:
		return types.Float64Null(), nil
	case types.Bool:
		return types.BoolNull(), nil
	case types.List:
		return types.ListNull(v.ElementType(context.Background())), nil
	case types.Set:
		return types.SetNull(v.ElementType(context.Background())), nil
	case types.Map:
		return types.MapNull(v.ElementType(context.Background())), nil
	case types.Object:
		return types.ObjectNull(v.AttributeTypes(context.Background())), nil
	case types.Dynamic:
		return types.DynamicNull(), nil
	}
	return nil, fmt.Errorf("unsupported attribute value type %T", current)
}

// nullAttrValueOfType returns the null value for the given attribute type.
func nullAttrValueOfType(attrType attr.Type) (attr.Value, error) {
	switch t := attrType.(type) {
	case basetypes.StringType:
		return types.StringNull(), nil
	case basetypes.Int64Type:
		return types.Int64Null(), nil
	case basetypes.Float64Type:
		return types.Float64Null(), nil
	case basetypes.BoolType:
		return types.BoolNull(), nil
	case basetypes.ListType:
		return types.ListNull(t.ElementType()), nil
	case basetypes.SetType:
		return types.SetNull(t.ElementType()), nil
	case basetypes.MapType:
		return types.MapNull(t.ElementType()), nil
	case basetypes.ObjectType:
		return types.ObjectNull(t.AttributeTypes()), nil
	case basetypes.DynamicType:
		return types.DynamicNull(), nil
	}
	return nil, fmt.Errorf("unsupported attribute type %T", attrType)
}

{{ if .IncludeXML }}
// mapToXML encodes a JSON-ready map as an XML document wrapped in a root
// element. Map keys are emitted as child elements in sorted order so the output
// is deterministic for the same input. Nested maps nest as child elements,
// arrays repeat the parent element name (the conventional XML array shape), and
// scalars become element character data. Custom XML element/attribute names and
// the OpenAPI xml keyword are out of scope (A2); the encoding is a best-effort
// body for APIs that accept application/xml. It is emitted only when a resource
// wired CRUD body serializes application/xml (N-37); JSON-only providers carry
// no dead XML serialization helpers.
func mapToXML(m map[string]any, root string) ([]byte, error) {
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	if err := writeXMLElement(enc, root, m); err != nil {
		return nil, err
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeXMLElement(enc *xml.Encoder, name string, v any) error {
	start := xml.StartElement{Name: xml.Name{Local: name}}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	if err := writeXMLValue(enc, name, v); err != nil {
		return err
	}
	return enc.EncodeToken(start.End())
}

func writeXMLValue(enc *xml.Encoder, name string, v any) error {
	switch x := v.(type) {
	case nil:
		return nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := writeXMLElement(enc, k, x[k]); err != nil {
				return err
			}
		}
		return nil
	case []any:
		// Repeat the parent element name for each item — the conventional XML
		// array shape, e.g. {"pets":[{"id":1}]} -> <name><name>...</name></name>.
		for _, item := range x {
			if err := writeXMLElement(enc, name, item); err != nil {
				return err
			}
		}
		return nil
	default:
		return enc.EncodeToken(xml.CharData(fmt.Sprintf("%v", x)))
	}
}
{{ end }}
// preserveStateIntoPlan copies attributes known in state into plan when the
// plan value is unknown, so an Update request body carries persisted computed
// values (e.g. an optimistic-concurrency version field) that the plan does not
// re-derive (G20). It is a no-op for attributes the plan already knows.
func preserveStateIntoPlan(plan, state any) {
	pv := reflect.ValueOf(plan)
	sv := reflect.ValueOf(state)
	if pv.Kind() == reflect.Pointer {
		pv = pv.Elem()
	}
	if sv.Kind() == reflect.Pointer {
		sv = sv.Elem()
	}
	if pv.Kind() != reflect.Struct || sv.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < pv.NumField(); i++ {
		pf := pv.Field(i)
		sf := sv.Field(i)
		if !pf.CanSet() {
			continue
		}
		pv_, pok := pf.Interface().(attr.Value)
		sv_, sok := sf.Interface().(attr.Value)
		if !pok || !sok {
			continue
		}
		if pv_.IsUnknown() && !sv_.IsUnknown() {
			pf.Set(sf)
		}
	}
}
`
