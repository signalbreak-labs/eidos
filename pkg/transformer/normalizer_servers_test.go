package transformer

import (
	"reflect"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/parser"
)

func TestNormalizeOperationServersInheritsGlobal(t *testing.T) {
	global := []Server{
		{URL: "https://api.example.com", Description: "Production"},
	}
	got := NormalizeOperationServers(global, nil, nil)
	want := []ir.ServerIR{
		{URL: "https://api.example.com", Description: "Production"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("global inheritance mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOperationServersPathOverridesGlobal(t *testing.T) {
	global := []Server{
		{URL: "https://api.example.com"},
	}
	pathItem := []Server{
		{URL: "https://api.example.com/v1"},
	}
	got := NormalizeOperationServers(global, pathItem, nil)
	want := []ir.ServerIR{
		{URL: "https://api.example.com/v1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("path override mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOperationServersOperationOverridesPath(t *testing.T) {
	global := []Server{
		{URL: "https://api.example.com"},
	}
	pathItem := []Server{
		{URL: "https://api.example.com/v1"},
	}
	operation := []Server{
		{URL: "https://staging.example.com"},
	}
	got := NormalizeOperationServers(global, pathItem, operation)
	want := []ir.ServerIR{
		{URL: "https://staging.example.com"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operation override mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOperationServersOperationEmptyMeansNone(t *testing.T) {
	global := []Server{
		{URL: "https://api.example.com"},
	}
	got := NormalizeOperationServers(global, nil, []Server{})
	if got != nil {
		t.Fatalf("explicit empty operation servers should yield nil, got %#v", got)
	}
}

func TestNormalizeOperationServersPathEmptyOverridesGlobal(t *testing.T) {
	global := []Server{
		{URL: "https://api.example.com"},
	}
	got := NormalizeOperationServers(global, []Server{}, nil)
	if got != nil {
		t.Fatalf("explicit empty path servers should yield nil, got %#v", got)
	}
}

func TestNormalizeOperationServersConvertsVariables(t *testing.T) {
	global := []Server{
		{
			URL:         "https://{region}.api.example.com",
			Description: "Regional",
			Variables: map[string]ServerVariable{
				"region": {
					Default:     "us-east-1",
					Enum:        []string{"us-east-1", "us-west-2"},
					Description: "Deployment region",
				},
			},
		},
	}

	got := NormalizeOperationServers(global, nil, nil)
	want := []ir.ServerIR{
		{
			URL:         "https://{region}.api.example.com",
			Description: "Regional",
			Variables: map[string]ir.ServerVariableIR{
				"region": {
					Default:     "us-east-1",
					Enum:        []string{"us-east-1", "us-west-2"},
					Description: "Deployment region",
				},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("variable conversion mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOperationServersReturnsCopy(t *testing.T) {
	global := []Server{
		{
			URL: "https://api.example.com",
			Variables: map[string]ServerVariable{
				"region": {Default: "us-east-1", Enum: []string{"us-east-1"}},
			},
		},
	}

	got := NormalizeOperationServers(global, nil, nil)
	got[0].Variables["region"] = ir.ServerVariableIR{Default: "changed"}

	if global[0].Variables["region"].Default != "us-east-1" {
		t.Fatalf("NormalizeOperationServers returned a value that shares state with the input")
	}
}

func TestNormalizeOperationServersAllNil(t *testing.T) {
	got := NormalizeOperationServers(nil, nil, nil)
	if got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestParserServersToTransformerConvertsVariables(t *testing.T) {
	in := []parser.Server{
		{
			URL:         "https://{region}.api.example.com",
			Description: "Regional",
			Variables: map[string]*parser.ServerVariable{
				"region": {
					Default:     "us-east-1",
					Enum:        []string{"us-east-1", "us-west-2"},
					Description: "Deployment region",
				},
			},
		},
	}
	got := parserServersToTransformer(in)
	want := []Server{
		{
			URL:         "https://{region}.api.example.com",
			Description: "Regional",
			Variables: map[string]ServerVariable{
				"region": {
					Default:     "us-east-1",
					Enum:        []string{"us-east-1", "us-west-2"},
					Description: "Deployment region",
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parserServersToTransformer mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestParserServersToTransformerNilAndEmpty(t *testing.T) {
	if got := parserServersToTransformer(nil); got != nil {
		t.Fatalf("parserServersToTransformer(nil) = %#v, want nil", got)
	}
	// An explicit empty (non-nil) slice must stay non-nil so
	// NormalizeOperationServers honors the empty override.
	if got := parserServersToTransformer([]parser.Server{}); got == nil {
		t.Fatal("parserServersToTransformer([]) = nil, want empty non-nil slice")
	}
}

func TestParserServersToTransformerSkipsNilVariable(t *testing.T) {
	in := []parser.Server{
		{
			URL: "https://api.example.com",
			Variables: map[string]*parser.ServerVariable{
				"region": nil,
			},
		},
	}
	got := parserServersToTransformer(in)
	if len(got) != 1 || len(got[0].Variables) != 0 {
		t.Fatalf("parserServersToTransformer with nil variable = %#v, want empty Variables", got)
	}
}
