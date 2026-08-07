package generator

import (
	"strings"
	"testing"
)

func TestReadme(t *testing.T) {
	cfg := BuildConfig{
		ProviderName: "mycloud",
		Namespace:    "acme",
	}

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{Readme(cfg)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := readFile(t, h.OutputDir, "README.md")
	for _, want := range []string{
		"# mycloud Terraform Provider",
		"registry.terraform.io/acme/mycloud",
		"prepared for publication",
		"Registry publishing is left to the operator",
		"does **not** automatically submit or list the provider",
		"make install",
		"dev_overrides",
		"~/.terraformrc",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("README.md missing %q\ncontent:\n%s", want, content)
		}
	}
}

func TestRegistryManifest(t *testing.T) {
	cfg := BuildConfig{
		ProviderName:     "mycloud",
		Namespace:        "mycloud",
		ProtocolVersions: []string{"6.0"},
	}

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{RegistryManifest(cfg)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := readFile(t, h.OutputDir, "terraform-registry-manifest.json")
	for _, want := range []string{
		"\"version\": 1",
		"\"protocol_versions\": [\"6.0\"]",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("terraform-registry-manifest.json missing %q\ncontent:\n%s", want, content)
		}
	}
}

func TestRegistryManifest_Defaults(t *testing.T) {
	cfg := BuildConfig{
		ProviderName: "mycloud",
		Namespace:    "mycloud",
	}

	h := Harness{OutputDir: t.TempDir()}
	if err := h.Generate([]File{RegistryManifest(cfg)}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := readFile(t, h.OutputDir, "terraform-registry-manifest.json")
	if !strings.Contains(content, "\"protocol_versions\": [\"6.0\"]") {
		t.Errorf("terraform-registry-manifest.json missing default protocol version\ncontent:\n%s", content)
	}
}
