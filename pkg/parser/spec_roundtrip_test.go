package parser

import (
	"encoding/json"
	"fmt"
	"testing"
)

// roundTripCase describes a JSON round-trip through a type that carries vendor
// extensions. new returns a fresh zero pointer, seed populates its known fields
// plus an x- extension, and check asserts the known field and extension
// survived the round-trip.
type roundTripCase struct {
	name  string
	new   func() any
	seed  func(v any)
	check func(v any) error
}

// extensionRoundTrips is the full set of extension-bearing spec types. Every
// one implements MarshalJSON/UnmarshalJSON through
// marshalWithExtensions/unmarshalExtensions; the zero-coverage members
// (Encoding, Example, Link, Discriminator, XML) are exactly the ones the parse
// pipeline never round-trips.
var extensionRoundTrips = []roundTripCase{
	{
		name: "Encoding",
		new:  func() any { return &Encoding{} },
		seed: func(v any) {
			e := v.(*Encoding)
			e.ContentType = "application/octet-stream"
			e.Style = "binary"
			e.Explode = true
			e.AllowReserved = true
			e.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			e := v.(*Encoding)
			if e.ContentType != "application/octet-stream" || e.Style != "binary" || !e.Explode || !e.AllowReserved {
				return fmt.Errorf("Encoding fields lost: %+v", e)
			}
			if e.Extensions["x-test"] != "yes" {
				return fmt.Errorf("Encoding extension lost: %v", e.Extensions)
			}
			return nil
		},
	},
	{
		name: "Example",
		new:  func() any { return &Example{} },
		seed: func(v any) {
			e := v.(*Example)
			e.Summary = "sum"
			e.Description = "desc"
			e.Value = map[string]any{"id": 1}
			e.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			e := v.(*Example)
			if e.Summary != "sum" || e.Description != "desc" {
				return fmt.Errorf("Example fields lost: %+v", e)
			}
			if e.Extensions["x-test"] != "yes" {
				return fmt.Errorf("Example extension lost: %v", e.Extensions)
			}
			return nil
		},
	},
	{
		name: "Link",
		new:  func() any { return &Link{} },
		seed: func(v any) {
			l := v.(*Link)
			l.OperationID = "getPet"
			l.Description = "link desc"
			l.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			l := v.(*Link)
			if l.OperationID != "getPet" || l.Description != "link desc" {
				return fmt.Errorf("Link fields lost: %+v", l)
			}
			if l.Extensions["x-test"] != "yes" {
				return fmt.Errorf("Link extension lost: %v", l.Extensions)
			}
			return nil
		},
	},
	{
		name: "Discriminator",
		new:  func() any { return &Discriminator{} },
		seed: func(v any) {
			d := v.(*Discriminator)
			d.PropertyName = "kind"
			d.Mapping = map[string]string{"cat": "#/definitions/Cat"}
			d.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			d := v.(*Discriminator)
			if d.PropertyName != "kind" || d.Mapping["cat"] != "#/definitions/Cat" {
				return fmt.Errorf("Discriminator fields lost: %+v", d)
			}
			if d.Extensions["x-test"] != "yes" {
				return fmt.Errorf("Discriminator extension lost: %v", d.Extensions)
			}
			return nil
		},
	},
	{
		name: "XML",
		new:  func() any { return &XML{} },
		seed: func(v any) {
			x := v.(*XML)
			x.Name = "pet"
			x.Namespace = "https://example.com"
			x.Prefix = "p"
			x.Attribute = true
			x.Wrapped = true
			x.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			x := v.(*XML)
			if x.Name != "pet" || x.Namespace != "https://example.com" || x.Prefix != "p" || !x.Attribute || !x.Wrapped {
				return fmt.Errorf("XML fields lost: %+v", x)
			}
			if x.Extensions["x-test"] != "yes" {
				return fmt.Errorf("XML extension lost: %v", x.Extensions)
			}
			return nil
		},
	},
	{
		name: "Tag",
		new:  func() any { return &Tag{} },
		seed: func(v any) {
			t := v.(*Tag)
			t.Name = "pets"
			t.Description = "tag desc"
			t.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			t := v.(*Tag)
			if t.Name != "pets" || t.Description != "tag desc" {
				return fmt.Errorf("Tag fields lost: %+v", t)
			}
			if t.Extensions["x-test"] != "yes" {
				return fmt.Errorf("Tag extension lost: %v", t.Extensions)
			}
			return nil
		},
	},
	{
		name: "ExternalDocs",
		new:  func() any { return &ExternalDocs{} },
		seed: func(v any) {
			d := v.(*ExternalDocs)
			d.URL = "https://example.com/docs"
			d.Description = "docs"
			d.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			d := v.(*ExternalDocs)
			if d.URL != "https://example.com/docs" || d.Description != "docs" {
				return fmt.Errorf("ExternalDocs fields lost: %+v", d)
			}
			if d.Extensions["x-test"] != "yes" {
				return fmt.Errorf("ExternalDocs extension lost: %v", d.Extensions)
			}
			return nil
		},
	},
	{
		name: "Response",
		new:  func() any { return &Response{} },
		seed: func(v any) {
			r := v.(*Response)
			r.Description = "ok"
			r.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			r := v.(*Response)
			if r.Description != "ok" {
				return fmt.Errorf("Response fields lost: %+v", r)
			}
			if r.Extensions["x-test"] != "yes" {
				return fmt.Errorf("Response extension lost: %v", r.Extensions)
			}
			return nil
		},
	},
	{
		name: "Header",
		new:  func() any { return &Header{} },
		seed: func(v any) {
			h := v.(*Header)
			h.Description = "rate limit"
			h.Required = true
			h.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			h := v.(*Header)
			if h.Description != "rate limit" || !h.Required {
				return fmt.Errorf("Header fields lost: %+v", h)
			}
			if h.Extensions["x-test"] != "yes" {
				return fmt.Errorf("Header extension lost: %v", h.Extensions)
			}
			return nil
		},
	},
	{
		name: "Components",
		new:  func() any { return &Components{} },
		seed: func(v any) {
			c := v.(*Components)
			c.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			c := v.(*Components)
			if c.Extensions["x-test"] != "yes" {
				return fmt.Errorf("Components extension lost: %v", c.Extensions)
			}
			return nil
		},
	},
	{
		name: "SecurityScheme",
		new:  func() any { return &SecurityScheme{} },
		seed: func(v any) {
			s := v.(*SecurityScheme)
			s.Type = "apiKey"
			s.Name = "X-Key"
			s.In = "header"
			s.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			s := v.(*SecurityScheme)
			if s.Type != "apiKey" || s.Name != "X-Key" || s.In != "header" {
				return fmt.Errorf("SecurityScheme fields lost: %+v", s)
			}
			if s.Extensions["x-test"] != "yes" {
				return fmt.Errorf("SecurityScheme extension lost: %v", s.Extensions)
			}
			return nil
		},
	},
	{
		name: "OAuthFlows",
		new:  func() any { return &OAuthFlows{} },
		seed: func(v any) {
			f := v.(*OAuthFlows)
			f.Implicit = &OAuthFlow{AuthorizationURL: "https://example.com/auth"}
			f.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			f := v.(*OAuthFlows)
			if f.Implicit == nil || f.Implicit.AuthorizationURL != "https://example.com/auth" {
				return fmt.Errorf("OAuthFlows fields lost: %+v", f)
			}
			if f.Extensions["x-test"] != "yes" {
				return fmt.Errorf("OAuthFlows extension lost: %v", f.Extensions)
			}
			return nil
		},
	},
	{
		name: "OAuthFlow",
		new:  func() any { return &OAuthFlow{} },
		seed: func(v any) {
			f := v.(*OAuthFlow)
			f.AuthorizationURL = "https://example.com/auth"
			f.TokenURL = "https://example.com/token"
			f.Scopes = map[string]string{"read": "Read"}
			f.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			f := v.(*OAuthFlow)
			if f.AuthorizationURL != "https://example.com/auth" || f.TokenURL != "https://example.com/token" || f.Scopes["read"] != "Read" {
				return fmt.Errorf("OAuthFlow fields lost: %+v", f)
			}
			if f.Extensions["x-test"] != "yes" {
				return fmt.Errorf("OAuthFlow extension lost: %v", f.Extensions)
			}
			return nil
		},
	},
	{
		name: "MediaType",
		new:  func() any { return &MediaType{} },
		seed: func(v any) {
			mt := v.(*MediaType)
			mt.Schema = &Schema{Ref: "#/components/schemas/Pet"}
			mt.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			mt := v.(*MediaType)
			if mt.Schema == nil || mt.Schema.Ref != "#/components/schemas/Pet" {
				return fmt.Errorf("MediaType fields lost: %+v", mt)
			}
			if mt.Extensions["x-test"] != "yes" {
				return fmt.Errorf("MediaType extension lost: %v", mt.Extensions)
			}
			return nil
		},
	},
	{
		name: "Schema",
		new:  func() any { return &Schema{} },
		seed: func(v any) {
			s := v.(*Schema)
			s.Type = "object"
			s.Title = "Pet"
			s.AdditionalProperties = &Schema{Type: "string"}
			s.Items = &Schema{Type: "string"}
			s.Extensions = map[string]any{"x-test": "yes"}
		},
		check: func(v any) error {
			s := v.(*Schema)
			if s.Type != "object" || s.Title != "Pet" {
				return fmt.Errorf("Schema fields lost: %+v", s)
			}
			// Items/AdditionalProperties are coerced back to *Schema.
			if s.AdditionalProperties == nil {
				return fmt.Errorf("Schema additionalProperties lost: %T %v", s.AdditionalProperties, s.AdditionalProperties)
			}
			if ap, ok := s.AdditionalProperties.(*Schema); !ok || ap.Type != "string" {
				return fmt.Errorf("Schema additionalProperties not *Schema: %T %v", s.AdditionalProperties, s.AdditionalProperties)
			}
			if it, ok := s.Items.(*Schema); !ok || it.Type != "string" {
				return fmt.Errorf("Schema items not *Schema: %T %v", s.Items, s.Items)
			}
			if s.Extensions["x-test"] != "yes" {
				return fmt.Errorf("Schema extension lost: %v", s.Extensions)
			}
			return nil
		},
	},
}

// TestExtensionJSONRoundTrip_Seeded drives the full round-trip for a value with
// an extension populated.
func TestExtensionJSONRoundTrip_Seeded(t *testing.T) {
	for _, tc := range extensionRoundTrips {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.new()
			tc.seed(v)
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			// Extension is flattened into the marshaled object.
			var raw map[string]any
			if err := json.Unmarshal(b, &raw); err != nil {
				t.Fatalf("unmarshal raw: %v", err)
			}
			if raw["x-test"] != "yes" {
				t.Errorf("x-test not present in marshaled JSON: %s", b)
			}
			// Round-trip into a fresh value.
			fresh := tc.new()
			if err := json.Unmarshal(b, fresh); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if err := tc.check(fresh); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestExtensionJSONRoundTrip_NoExtensions covers the marshalWithExtensions
// early-return (empty extension map) and that unmarshaling JSON without x- keys
// leaves Extensions nil.
func TestExtensionJSONRoundTrip_NoExtensions(t *testing.T) {
	for _, tc := range extensionRoundTrips {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.new()
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			fresh := tc.new()
			if err := json.Unmarshal(b, fresh); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			// Verify the fresh value's Extensions field is nil via a seed-less check.
			switch f := fresh.(type) {
			case *Encoding:
				if f.Extensions != nil {
					t.Errorf("unexpected extensions: %v", f.Extensions)
				}
			case *Link:
				if f.Extensions != nil {
					t.Errorf("unexpected extensions: %v", f.Extensions)
				}
			default:
				// All extension-bearing types marshal `{}` when empty; confirm
				// the output is still valid JSON for the remaining types.
				var obj map[string]any
				if err := json.Unmarshal(b, &obj); err != nil {
					t.Errorf("output not an object: %v", err)
				}
			}
		})
	}
}

// TestExtensionJSONRoundTrip_InvalidInput asserts each UnmarshalJSON returns an
// error on malformed input rather than silently succeeding.
func TestExtensionJSONRoundTrip_InvalidInput(t *testing.T) {
	for _, tc := range extensionRoundTrips {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte("{"), tc.new()); err == nil {
				t.Error("expected error for malformed JSON")
			}
		})
	}
}

// TestSchemaCoerceAnySchema directly exercises coerceAnySchema's three inputs:
// a plain map (converted to *Schema), a non-map (passed through), and nil.
func TestSchemaCoerceAnySchema(t *testing.T) {
	if got := coerceAnySchema(map[string]any{"type": "string"}); got == nil {
		t.Error("map input should coerce to *Schema")
	} else if s, ok := got.(*Schema); !ok || s.Type != "string" {
		t.Errorf("coerceAnySchema(map) = %T %v", got, got)
	}
	// A malformed value inside the map that cannot marshal leaves it unchanged.
	if got := coerceAnySchema(map[string]any{"bad": func() {}}); got == nil {
		t.Error("unmarshalable map should pass through unchanged")
	}
	if got := coerceAnySchema(true); got != true {
		t.Errorf("boolean input = %v, want true", got)
	}
	if got := coerceAnySchema(nil); got != nil {
		t.Errorf("nil input = %v, want nil", got)
	}
}

// TestMarshalWithExtensions_NoExtensions covers the len(extensions)==0
// early-return path directly.
func TestMarshalWithExtensions_NoExtensions(t *testing.T) {
	b, err := marshalWithExtensions(&XML{Name: "pet"}, nil)
	if err != nil {
		t.Fatalf("marshalWithExtensions: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["name"] != "pet" {
		t.Errorf("marshalWithExtensions output = %v", got)
	}
}

// TestUnmarshalExtensions asserts x- keys are extracted and non-x- keys are
// ignored, plus the malformed-input error path.
func TestUnmarshalExtensions(t *testing.T) {
	data := []byte(`{"name": "pet", "x-test": "yes", "plain": 1}`)
	var ext map[string]any
	if err := unmarshalExtensions(data, &ext); err != nil {
		t.Fatalf("unmarshalExtensions: %v", err)
	}
	if ext["x-test"] != "yes" {
		t.Errorf("extensions = %v, want x-test=yes", ext)
	}
	if _, ok := ext["plain"]; ok {
		t.Errorf("non-x- key leaked into extensions: %v", ext)
	}
	// No x- keys leaves the map nil (unset).
	var empty map[string]any
	if err := unmarshalExtensions([]byte(`{"a": 1}`), &empty); err != nil {
		t.Fatalf("unmarshalExtensions no-ext: %v", err)
	}
	if empty != nil {
		t.Errorf("expected nil map, got %v", empty)
	}
	if err := unmarshalExtensions([]byte(`{`), &empty); err == nil {
		t.Error("expected error for malformed JSON")
	}
}
