package transformer

import (
	"reflect"
	"regexp"
	"testing"
	"testing/quick"
)

func TestToSnakeCase(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"getPet", "get_pet"},
		{"GetPet", "get_pet"},
		{"get_pet", "get_pet"},
		{"get-pet", "get_pet"},
		{"get pet", "get_pet"},
		{"get.pet", "get_pet"},
		{"HTTPResponse", "http_response"},
		{"getHTTPResponse", "get_http_response"},
		{"petID", "pet_id"},
		{"getPetByID", "get_pet_by_id"},
		{"pet_v2", "pet_v2"},
		{"api_v2", "api_v2"},
		{"URL", "url"},
		{"", ""},
		{"__mixed--Case__", "mixed_case"},
		{" already_snake ", "already_snake"},
		// Leading-digit inputs must not produce an identifier starting with a
		// digit (invalid in Go and HCL); an "x" prefix is added (L-99).
		{"2fa", "x2_fa"},
		{"2FA", "x2_fa"},
		{"2024_report", "x2024_report"},
		{"123", "x123"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := ToSnakeCase(tc.input); got != tc.want {
				t.Fatalf("ToSnakeCase(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestToProviderTypeName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"getPet", "get-pet"},
		{"GetPet", "get-pet"},
		{"get_pet", "get-pet"},
		{"get-pet", "get-pet"},
		{"get pet", "get-pet"},
		{"get.pet", "get-pet"},
		{"HTTPResponse", "http-response"},
		{"getHTTPResponse", "get-http-response"},
		{"petID", "pet-id"},
		{"getPetByID", "get-pet-by-id"},
		{"pet_v2", "pet-v2"},
		{"api_v2", "api-v2"},
		{"URL", "url"},
		{"", ""},
		{"__mixed--Case__", "mixed-case"},
		{" already_snake ", "already-snake"},
		// Terraform provider type names reject underscores and dots, so a
		// multi-word title must produce kebab-case, not snake_case.
		{"Mycloud Pets", "mycloud-pets"},
		{"AllOf Nesting API", "all-of-nesting-api"},
		// Leading-digit inputs must not produce an identifier starting with a
		// digit (invalid in Go and HCL); an "x" prefix is added (L-99).
		{"2fa", "x2-fa"},
		{"2FA", "x2-fa"},
		{"2024_report", "x2024-report"},
		{"123", "x123"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := ToProviderTypeName(tc.input); got != tc.want {
				t.Fatalf("ToProviderTypeName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestToPascalCase(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"getPet", "GetPet"},
		{"get_pet", "GetPet"},
		{"get-pet", "GetPet"},
		{"get pet", "GetPet"},
		{"get.pet", "GetPet"},
		{"GetPet", "GetPet"},
		{"HTTPResponse", "HttpResponse"},
		{"getHTTPResponse", "GetHttpResponse"},
		{"petID", "PetId"},
		{"pet_v2", "PetV2"},
		{"", ""},
		{"__mixed--Case__", "MixedCase"},
		{" already_snake ", "AlreadySnake"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := ToPascalCase(tc.input); got != tc.want {
				t.Fatalf("ToPascalCase(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestDeriveOperationID(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/pets", "get_pets"},
		{"POST", "/pets", "post_pets"},
		{"GET", "/pets/{petId}", "get_pets"},
		{"POST", "/pets/{petId}/reboot", "post_pets_reboot"},
		{"GET", "/pets/{petId}/owner/{ownerId}", "get_pets_owner"},
		{"GET", "pets", "get_pets"},
		{"", "/pets", "pets"},
		{"DELETE", "", "delete"},
		{"", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.method+"_"+tc.path, func(t *testing.T) {
			if got := DeriveOperationID(tc.method, tc.path); got != tc.want {
				t.Fatalf("DeriveOperationID(%q, %q) = %q, want %q", tc.method, tc.path, got, tc.want)
			}
		})
	}
}

func TestNormalizeOperationID(t *testing.T) {
	cases := []struct {
		name   string
		opID   string
		method string
		path   string
		want   string
	}{
		{"explicit id", "getPet", "", "", "get_pet"},
		{"explicit id with spaces", "  GetPet  ", "", "", "get_pet"},
		{"derived", "", "GET", "/pets", "get_pets"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeOperationID(tc.opID, tc.method, tc.path); got != tc.want {
				t.Fatalf("NormalizeOperationID(%q, %q, %q) = %q, want %q", tc.opID, tc.method, tc.path, got, tc.want)
			}
		})
	}
}

func TestSplitWordsDeterminism(t *testing.T) {
	// Multiple separators and mixed formatting should still produce the same words.
	first := splitWords("get-PetByID")
	second := splitWords("getPetByID")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("splitWords not deterministic for equivalent identifiers: %v vs %v", first, second)
	}
}

// snakeCaseRE matches valid ToSnakeCase output: Unicode letters and digits
// separated by underscores, with no leading or trailing underscores and no
// consecutive underscores. Go identifiers and Terraform type names allow
// Unicode letters, so the test accepts any letter/digit rune, not just ASCII.
var snakeCaseRE = regexp.MustCompile(`^([\p{L}\p{N}]+(_[\p{L}\p{N}]+)*)?$`)

// TestToSnakeCaseProperties uses testing/quick to assert that ToSnakeCase is
// idempotent and that its output consists of Unicode letters/digits separated by
// underscores with no leading, trailing, or consecutive underscores.
func TestToSnakeCaseProperties(t *testing.T) {
	idempotent := func(s string) bool {
		once := ToSnakeCase(s)
		twice := ToSnakeCase(once)
		return once == twice
	}
	format := func(s string) bool {
		return snakeCaseRE.MatchString(ToSnakeCase(s))
	}

	if err := quick.Check(idempotent, nil); err != nil {
		t.Errorf("ToSnakeCase idempotence failed: %v", err)
	}
	if err := quick.Check(format, nil); err != nil {
		t.Errorf("ToSnakeCase output format failed: %v", err)
	}
}

// TestNormalizeOperationIDProperties uses testing/quick to assert that
// NormalizeOperationID and DeriveOperationID are stable and produce valid
// snake_case identifiers.
func TestNormalizeOperationIDProperties(t *testing.T) {
	stable := func(opID, method, path string) bool {
		first := NormalizeOperationID(opID, method, path)
		second := NormalizeOperationID(first, method, path)
		return first == second && snakeCaseRE.MatchString(first)
	}
	deriveStable := func(method, path string) bool {
		first := DeriveOperationID(method, path)
		second := DeriveOperationID(method, path)
		return first == second && snakeCaseRE.MatchString(first)
	}

	if err := quick.Check(stable, nil); err != nil {
		t.Errorf("NormalizeOperationID stability failed: %v", err)
	}
	if err := quick.Check(deriveStable, nil); err != nil {
		t.Errorf("DeriveOperationID stability failed: %v", err)
	}
}

func FuzzToSnakeCase(f *testing.F) {
	seeds := []string{"getPet", "GetPet", "get_pet", "get-pet", "get pet", "HTTPResponse", "petID", "v2alpha", "", "__a--B__"}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := ToSnakeCase(s)
		if ToSnakeCase(got) != got {
			t.Fatalf("ToSnakeCase not idempotent: %q -> %q -> %q", s, got, ToSnakeCase(got))
		}
		if !snakeCaseRE.MatchString(got) {
			t.Fatalf("ToSnakeCase(%q) = %q, does not match expected format", s, got)
		}
	})
}

func FuzzNormalizeOperationID(f *testing.F) {
	seeds := []struct {
		opID, method, path string
	}{
		{"getPet", "GET", "/pets"},
		{"", "POST", "/pets/{id}/reboot"},
		{"rotateDatabaseCredentials", "", ""},
		{"", "", ""},
	}
	for _, s := range seeds {
		f.Add(s.opID, s.method, s.path)
	}
	f.Fuzz(func(t *testing.T, opID, method, path string) {
		got := NormalizeOperationID(opID, method, path)
		if NormalizeOperationID(got, method, path) != got {
			t.Fatalf("NormalizeOperationID not stable: %q/%q/%q -> %q -> %q", opID, method, path, got, NormalizeOperationID(got, method, path))
		}
		if !snakeCaseRE.MatchString(got) {
			t.Fatalf("NormalizeOperationID(%q, %q, %q) = %q, does not match expected format", opID, method, path, got)
		}
	})
}
