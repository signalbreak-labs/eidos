package transformer

import (
	"reflect"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestInferEphemeralResourcesWriteOnlyResponse(t *testing.T) {
	op := Operation{
		Method:       MethodPost,
		Path:         "/credentials/temporary",
		OperationID:  "createTemporaryCredential",
		ResponseBody: true,
		ResponseSchema: &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"access_key_id":     {Type: "string"},
				"secret_access_key": {Type: "string", WriteOnly: true},
				"session_token":     {Type: "string", WriteOnly: true},
			},
		},
	}
	pathOps := map[string]map[HTTPMethod]Operation{
		"/credentials/temporary": {
			MethodPost: op,
		},
	}

	er := InferEphemeralResources(pathOps)
	if len(er) != 1 {
		t.Fatalf("expected 1 ephemeral resource, got %d", len(er))
	}

	want := EphemeralResourceIR{
		Name:         "create_temporary_credential",
		FullName:     "Create Temporary Credential",
		TypeName:     "create_temporary_credential",
		Description:  "createTemporaryCredential",
		ConfigSchema: ir.ObjectSchemaIR{},
		ResultSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "access_key_id",
					Schema:   ir.SchemaIR{Type: ir.TypeString},
					Computed: true,
				},
				{
					Name:      "secret_access_key",
					Schema:    ir.SchemaIR{Type: ir.TypeString, Sensitive: true},
					Computed:  true,
					Sensitive: true,
				},
				{
					Name:      "session_token",
					Schema:    ir.SchemaIR{Type: ir.TypeString, Sensitive: true},
					Computed:  true,
					Sensitive: true,
				},
			},
		},
		OpenMapping:     op,
		SourceOperation: "createTemporaryCredential",
	}

	if !reflect.DeepEqual(er[0], want) {
		t.Errorf("InferEphemeralResources() = %+v, want %+v", er[0], want)
	}
}

func TestInferEphemeralResourcesPasswordFormat(t *testing.T) {
	op := Operation{
		Method:       MethodPost,
		Path:         "/auth/token",
		ResponseBody: true,
		ResponseSchema: &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"token": {Type: "string", Format: "password"},
			},
		},
	}
	pathOps := map[string]map[HTTPMethod]Operation{
		"/auth/token": {
			MethodPost: op,
		},
	}

	er := InferEphemeralResources(pathOps)
	if len(er) != 1 {
		t.Fatalf("expected 1 ephemeral resource, got %d", len(er))
	}
	if er[0].Name != "token" {
		t.Errorf("expected name 'token', got %q", er[0].Name)
	}
}

func TestInferEphemeralResourcesKeywordPath(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/sessions": {
			MethodPost: op(MethodPost, "/sessions"),
		},
	}

	er := InferEphemeralResources(pathOps)
	if len(er) != 1 {
		t.Fatalf("expected 1 ephemeral resource, got %d", len(er))
	}
	if er[0].Name != "sessions" {
		t.Errorf("expected name 'sessions', got %q", er[0].Name)
	}
}

// TestInferEphemeralResourcesSubstringFalsePositives locks in the L-94 fix:
// paths whose segments merely contain a cue as a prefix of a longer word
// ("/passwordless-policy" contains "password"; "/tokenizers" contains
// "token") must NOT be treated as ephemeral candidates.
func TestInferEphemeralResourcesSubstringFalsePositives(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/passwordless-policy": {
			MethodPost: {Method: MethodPost, Path: "/passwordless-policy", OperationID: "configurePasswordlessPolicy"},
		},
		"/tokenizers": {
			MethodPost: {Method: MethodPost, Path: "/tokenizers", OperationID: "createTokenizer"},
		},
	}

	er := InferEphemeralResources(pathOps)
	if len(er) != 0 {
		t.Errorf("expected no ephemeral resources for compound-word paths, got %v", er)
	}
}

func TestInferEphemeralResourcesDetectsRenewAndClose(t *testing.T) {
	open := Operation{
		Method:       MethodPost,
		Path:         "/credentials/temporary",
		OperationID:  "createTemporaryCredential",
		ResponseBody: true,
		ResponseSchema: &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"secret": {Type: "string", WriteOnly: true},
			},
		},
	}
	renew := Operation{Method: MethodPost, Path: "/credentials/temporary/renew"}
	closeOp := Operation{Method: MethodDelete, Path: "/credentials/temporary/close"}

	pathOps := map[string]map[HTTPMethod]Operation{
		"/credentials/temporary": {
			MethodPost: open,
		},
		"/credentials/temporary/renew": {
			MethodPost: renew,
		},
		"/credentials/temporary/close": {
			MethodDelete: closeOp,
		},
	}

	er := InferEphemeralResources(pathOps)
	if len(er) != 1 {
		t.Fatalf("expected 1 ephemeral resource, got %d", len(er))
	}
	res := er[0]
	if !res.HasRenew {
		t.Errorf("expected HasRenew=true")
	}
	if !res.HasClose {
		t.Errorf("expected HasClose=true")
	}
	if res.RenewMapping == nil || res.RenewMapping.Path != renew.Path {
		t.Errorf("unexpected renew mapping: %v", res.RenewMapping)
	}
	if res.CloseMapping == nil || res.CloseMapping.Path != closeOp.Path {
		t.Errorf("unexpected close mapping: %v", res.CloseMapping)
	}
}

func TestInferEphemeralResourcesIgnoresNonCandidates(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/pets": {
			MethodGet: op(MethodGet, "/pets"),
		},
		"/pets/{petId}": {
			MethodGet: op(MethodGet, "/pets/{petId}"),
		},
	}

	er := InferEphemeralResources(pathOps)
	if len(er) != 0 {
		t.Errorf("expected no ephemeral resources, got %v", er)
	}
}

// TestInferEphemeralResourcesNoDuplicateWithCRUDCreate locks in the M-38 fix:
// a POST on a collection that has a paired instance subpath is a managed-resource
// Create and must NOT also be emitted as an ephemeral resource, even though the
// path matches a credential cue.
func TestInferEphemeralResourcesNoDuplicateWithCRUDCreate(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/tokens": {
			MethodPost: {Method: MethodPost, Path: "/tokens", OperationID: "createToken"},
		},
		"/tokens/{tokenId}": {
			MethodGet:    {Method: MethodGet, Path: "/tokens/{tokenId}", OperationID: "showToken"},
			MethodDelete: {Method: MethodDelete, Path: "/tokens/{tokenId}", OperationID: "deleteToken"},
		},
	}

	er := InferEphemeralResources(pathOps)
	if len(er) != 0 {
		t.Errorf("expected POST /tokens to be a CRUD Create (not ephemeral), got %v", er)
	}
}

// TestInferEphemeralResourcesNoDuplicateGETDataSource locks in the M-38 fix: a
// GET on a credential-cued collection is a data source, not an ephemeral open,
// and must not be emitted as an ephemeral resource.
func TestInferEphemeralResourcesNoDuplicateGETDataSource(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/sessions": {
			MethodGet: {Method: MethodGet, Path: "/sessions", OperationID: "listSessions"},
		},
	}

	er := InferEphemeralResources(pathOps)
	if len(er) != 0 {
		t.Errorf("expected GET /sessions to be a data source (not ephemeral), got %v", er)
	}
}

// TestEphemeralResponse_UniqueItemsIsSet locks in A1: an ephemeral open
// response whose property is an array with uniqueItems: true maps to a Computed
// Set attribute via the shared schemaIRFromSpec mapper (which now has an array
// branch). Both the object-property array and the top-level array cases are
// covered.
func TestEphemeralResponse_UniqueItemsIsSet(t *testing.T) {
	t.Run("object property array", func(t *testing.T) {
		spec := &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"scopes": {Type: "array", UniqueItems: true, Items: &SchemaSpec{Type: "string"}},
			},
		}
		schema := ResultSchemaFromResponse(spec)
		for _, a := range schema.Attributes {
			if a.Name == "scopes" {
				if a.Schema.Collection == nil || a.Schema.Collection.Kind != ir.Set {
					t.Fatalf("expected scopes Set collection, got %+v", a.Schema)
				}
				if !a.Computed {
					t.Errorf("ephemeral response attribute must be Computed")
				}
				return
			}
		}
		t.Fatalf("expected a scopes attribute, got %+v", schema.Attributes)
	})

	t.Run("top-level array", func(t *testing.T) {
		spec := &SchemaSpec{Type: "array", UniqueItems: true, Items: &SchemaSpec{Type: "integer"}}
		schema := ResultSchemaFromResponse(spec)
		if len(schema.Attributes) != 1 || schema.Attributes[0].Name != "result" {
			t.Fatalf("expected a single result attribute, got %+v", schema.Attributes)
		}
		a := schema.Attributes[0]
		if a.Schema.Collection == nil || a.Schema.Collection.Kind != ir.Set {
			t.Fatalf("expected result Set collection, got %+v", a.Schema)
		}
		if a.Schema.Collection.ElementType.Type != ir.TypeInt {
			t.Errorf("element type = %q, want int", a.Schema.Collection.ElementType.Type)
		}
	})
}
