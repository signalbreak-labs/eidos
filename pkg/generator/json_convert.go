package generator

// JSONConvertFile returns the generated internal/provider/json_convert.go file
// containing generic conversions between the Terraform Plugin Framework model
// structs and JSON-ready maps. Resource CRUD bodies that are wired to the
// generated API client use these helpers to build request bodies and to apply
// response payloads back to Terraform state, so no per-attribute mapping code
// has to be generated. The file is a static template like the internal/client
// package files: its content does not depend on the provider IR.
func JSONConvertFile() File {
	return Template("internal/provider/json_convert.go", jsonConvertGoTemplate, nil)
}

const jsonConvertGoTemplate = `package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"reflect"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// modelToJSONMap converts a generated Terraform model struct into a JSON-ready
// map keyed by tfsdk attribute name. Null and unknown attribute values are
// omitted so optional attributes are not sent to the API. Dynamic attribute
// values cannot be represented as JSON and produce an error instead of being
// silently dropped.
func modelToJSONMap(model any) (map[string]any, error) {
	v := reflect.ValueOf(model)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct model, got %s", v.Kind())
	}
	t := v.Type()
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
		converted, keep, err := attrValueToJSON(value)
		if err != nil {
			return nil, fmt.Errorf("attribute %q: %w", name, err)
		}
		if keep {
			out[wire] = converted
		}
	}
	return out, nil
}

// applyJSONToModel updates the fields of a generated Terraform model struct
// from a decoded JSON object keyed by tfsdk attribute name. Keys absent from
// data leave the corresponding fields untouched, so attributes the API does
// not return keep their current values.
func applyJSONToModel(model any, data map[string]any) error {
	v := reflect.ValueOf(model)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("expected a struct model, got %s", v.Kind())
	}
	t := v.Type()
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
		raw, ok := data[name]
		if !ok {
			raw, ok = data[wire]
		}
		if !ok {
			continue
		}
		field := v.Field(i)
		current, ok := field.Interface().(attr.Value)
		if !ok {
			continue
		}
		converted, err := jsonToAttrValue(current, raw)
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

// attrValueToJSON converts a framework attribute value to a JSON-ready Go
// value. The boolean result reports whether the value should be included in
// the output; null and unknown values are excluded.
func attrValueToJSON(value attr.Value) (any, bool, error) {
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
	case types.List:
		return elementsToJSON(v.Elements())
	case types.Set:
		return elementsToJSON(v.Elements())
	case types.Map:
		return stringMapToJSON(v.Elements())
	case types.Object:
		return stringMapToJSON(v.Attributes())
	case types.Dynamic:
		// A dynamic attribute wraps an arbitrary attr.Value; convert the
		// wrapped value so the request body carries the practitioner's dynamic
		// payload (G18).
		return attrValueToJSON(v.UnderlyingValue())
	}
	return nil, false, fmt.Errorf("unsupported attribute value type %T", value)
}

func elementsToJSON(elements []attr.Value) (any, bool, error) {
	out := make([]any, 0, len(elements))
	for _, element := range elements {
		converted, keep, err := attrValueToJSON(element)
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

func stringMapToJSON(values map[string]attr.Value) (any, bool, error) {
	out := make(map[string]any, len(values))
	for name, value := range values {
		converted, keep, err := attrValueToJSON(value)
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
// null value for the attribute type.
func jsonToAttrValue(current attr.Value, raw any) (attr.Value, error) {
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
		elements, err := jsonToElements(v.ElementType(context.Background()), raw)
		if err != nil {
			return nil, err
		}
		list, diags := types.ListValue(v.ElementType(context.Background()), elements)
		if diags.HasError() {
			return nil, fmt.Errorf("%s", diags.Errors()[0].Detail())
		}
		return list, nil
	case types.Set:
		elements, err := jsonToElements(v.ElementType(context.Background()), raw)
		if err != nil {
			return nil, err
		}
		set, diags := types.SetValue(v.ElementType(context.Background()), elements)
		if diags.HasError() {
			return nil, fmt.Errorf("%s", diags.Errors()[0].Detail())
		}
		return set, nil
	case types.Map:
		values, err := jsonToStringMap(v.ElementType(context.Background()), raw)
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
			converted, err := jsonToAttrValueOfType(attrType, obj[name])
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
		// A dynamic attribute carries arbitrary JSON; round-trip it through
		// tftypes so the value is preserved verbatim (G18).
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("could not marshal dynamic value: %w", err)
		}
		tv, err := tftypes.ValueFromJSON(b, tftypes.DynamicPseudoType)
		if err != nil {
			return nil, fmt.Errorf("could not parse dynamic value: %w", err)
		}
		return types.DynamicType.ValueFromTerraform(context.Background(), tv)
	}
	return nil, fmt.Errorf("unsupported attribute value type %T", current)
}

// jsonToAttrValueOfType converts a decoded JSON value into a framework
// attribute value of the given type.
func jsonToAttrValueOfType(attrType attr.Type, raw any) (attr.Value, error) {
	null, err := nullAttrValueOfType(attrType)
	if err != nil {
		return nil, err
	}
	return jsonToAttrValue(null, raw)
}

func jsonToElements(elementType attr.Type, raw any) ([]attr.Value, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON array, got %T", raw)
	}
	elements := make([]attr.Value, 0, len(items))
	for _, item := range items {
		converted, err := jsonToAttrValueOfType(elementType, item)
		if err != nil {
			return nil, err
		}
		elements = append(elements, converted)
	}
	return elements, nil
}

func jsonToStringMap(elementType attr.Type, raw any) (map[string]attr.Value, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON object, got %T", raw)
	}
	values := make(map[string]attr.Value, len(obj))
	for name, item := range obj {
		converted, err := jsonToAttrValueOfType(elementType, item)
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

// mapToXML encodes a JSON-ready map as an XML document wrapped in a root
// element. Map keys are emitted as child elements in sorted order so the output
// is deterministic for the same input. Nested maps nest as child elements,
// arrays repeat the parent element name (the conventional XML array shape), and
// scalars become element character data. Custom XML element/attribute names and
// the OpenAPI xml keyword are out of scope (A2); the encoding is a best-effort
// body for APIs that accept application/xml.
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
