package transformer

import (
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func sensitiveAttr(name, wire string, typ ir.PrimitiveType) ir.AttributeIR {
	return ir.AttributeIR{Name: name, WireName: wire, Schema: ir.SchemaIR{Type: typ}}
}

func TestLooksSensitive_SubstringKeywords(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"httpPassword", true},
		{"password", true},
		{"fmpasswordEncrypted", true},
		{"sharedSecret", true},
		{"applicationSecretKey", true},
		{"secretKey", true},
		{"apiKey", true},
		{"accessKey", true},
		{"locationPrivateKey", true},
		{"clientSecret", true},
		{"credentials", true},
		{"adminPassword", true},
		{"ksPassword", true},
	}
	for _, c := range cases {
		got := looksSensitive(c.name, ir.SchemaIR{Type: ir.TypeString})
		if got != c.want {
			t.Errorf("looksSensitive(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLooksSensitive_TokenSuffix(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"token", true},
		{"access_token", true},
		{"auth_token", true},
		{"key_token", true},
		{"api_token", true},
		{"bearer_token", true},
		// Metadata about a token, not the token value itself: not sensitive.
		{"token_name", false},
		{"token_id", false},
		{"token_type", false},
		{"token_endpoint", false},
		{"tokenName", false}, // camelCase -> lowercased "tokenname", no _ split
	}
	for _, c := range cases {
		got := looksSensitive(c.name, ir.SchemaIR{Type: ir.TypeString})
		if got != c.want {
			t.Errorf("looksSensitive(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLooksSensitive_NonStringTypesExcluded(t *testing.T) {
	// Boolean/integer flag fields whose names contain "password" must not be
	// redacted — they are settings, not secrets.
	cases := []struct {
		name string
		typ  ir.PrimitiveType
	}{
		{"allowBlankPassword", ir.TypeBool},
		{"securePasswords", ir.TypeBool},
		{"passwordSet", ir.TypeBool},
		{"minPasswordLen", ir.TypeInt},
		{"privateKey", ir.TypeBool}, // boolean flag, not the key material
	}
	for _, c := range cases {
		got := looksSensitive(c.name, ir.SchemaIR{Type: c.typ})
		if got {
			t.Errorf("looksSensitive(%q, %v) = true, want false (non-string)", c.name, c.typ)
		}
	}
}

func TestLooksSensitive_CollectionsAndObjectsExcluded(t *testing.T) {
	// An array of token entities must not redact the whole collection.
	arr := ir.SchemaIR{Type: ir.TypeString, Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}}
	if looksSensitive("fmUserTokenEntities", arr) {
		t.Errorf("array attribute must not be sensitive")
	}
	// An object attribute is not a scalar secret.
	obj := ir.SchemaIR{Type: ir.TypeString, Attributes: []ir.AttributeIR{sensitiveAttr("inner", "inner", ir.TypeString)}}
	if looksSensitive("credentialsBag", obj) {
		t.Errorf("object attribute must not be sensitive")
	}
}

func TestLooksSensitive_FormatPassword(t *testing.T) {
	if !looksSensitive("value", ir.SchemaIR{Type: ir.TypeString, Format: "password"}) {
		t.Errorf("format:password string must be sensitive")
	}
	// Non-string format:password is ignored (type guard wins).
	if looksSensitive("value", ir.SchemaIR{Type: ir.TypeBool, Format: "password"}) {
		t.Errorf("non-string format:password must not be sensitive")
	}
}

func TestInferSensitiveAttributes_MarksAndPreserves(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			sensitiveAttr("password", "password", ir.TypeString),
			sensitiveAttr("name", "name", ir.TypeString),
			sensitiveAttr("allow_blank_password", "allowBlankPassword", ir.TypeBool),
			// Already sensitive (e.g. security-scheme inference): preserved, not double-set.
			{Name: "api_key", WireName: "api_key", Schema: ir.SchemaIR{Type: ir.TypeString}, Sensitive: true},
		},
	}
	InferSensitiveAttributes(obj)

	want := map[string]bool{"password": true, "name": false, "allow_blank_password": false, "api_key": true}
	for _, a := range obj.Attributes {
		if got, ok := want[a.Name]; ok && a.Sensitive != got {
			t.Errorf("attr %q Sensitive = %v, want %v", a.Name, a.Sensitive, got)
		}
	}
	// The schema flag must mirror the attribute flag for the password attr.
	pw := obj.Attributes[0]
	if !pw.Sensitive || !pw.Schema.Sensitive {
		t.Errorf("password attr and schema must both be Sensitive, got attr=%v schema=%v", pw.Sensitive, pw.Schema.Sensitive)
	}
}

func TestInferSensitiveAttributes_RecursesNested(t *testing.T) {
	// A nested object inside a list collection carries a password field.
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name:     "mail_server",
				WireName: "mailServer",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind: ir.List,
						ElementType: ir.SchemaIR{
							Attributes: []ir.AttributeIR{
								sensitiveAttr("password", "password", ir.TypeString),
								sensitiveAttr("host", "host", ir.TypeString),
							},
						},
					},
				},
			},
		},
	}
	InferSensitiveAttributes(obj)

	elem := obj.Attributes[0].Schema.Collection.ElementType
	pw := elem.Attributes[0]
	if !pw.Sensitive {
		t.Errorf("nested password must be Sensitive after recursion, got %+v", pw)
	}
	host := elem.Attributes[1]
	if host.Sensitive {
		t.Errorf("nested host must not be Sensitive, got %+v", host)
	}
}

func TestWarnUnmarkableSensitive_EmitsWarning(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			sensitiveAttr("password", "password", ir.TypeString),
			sensitiveAttr("name", "name", ir.TypeString),
		},
	}
	var diags diagnostics.Diagnostics
	WarnUnmarkableSensitive(obj, "action", "do_thing", &diags)
	if len(diags) != 1 {
		t.Fatalf("expected 1 warning for the password attr, got %d: %+v", len(diags), diags)
	}
	if diags[0].Severity != diagnostics.Warning {
		t.Errorf("expected Warning severity, got %v", diags[0].Severity)
	}
	if !contains(diags[0].Detail, "password") {
		t.Errorf("warning detail should name the attribute, got %q", diags[0].Detail)
	}
	// The schema must be unchanged (no Sensitive set).
	if obj.Attributes[0].Sensitive {
		t.Errorf("WarnUnmarkableSensitive must not modify the schema")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
