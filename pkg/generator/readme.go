package generator

// Readme returns the generated README.md file for the provider.
func Readme(cfg BuildConfig) File {
	return Template("README.md", readmeTemplate, map[string]any{
		"ProviderName":  cfg.providerName(),
		"Namespace":     cfg.namespace(),
		"SourceAddress": cfg.sourceAddress(),
		"GoVersion":     cfg.goVersion(),
		"Tick":          "`",
	})
}

const readmeTemplate = `# {{.ProviderName}} Terraform Provider

The {{.Tick}}{{.ProviderName}}{{.Tick}} Terraform provider is used to manage resources on {{.Tick}}{{.SourceAddress}}{{.Tick}}.

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.0
- [Go](https://golang.org/doc/install) >= {{.GoVersion}}

## Development

Build and install the provider locally:

{{.Tick}}{{.Tick}}{{.Tick}}shell
make install
{{.Tick}}{{.Tick}}{{.Tick}}

To test the provider without publishing it, add a {{.Tick}}dev_overrides{{.Tick}} block to {{.Tick}}~/.terraformrc{{.Tick}}:

{{.Tick}}{{.Tick}}{{.Tick}}hcl
provider_installation {
  dev_overrides {
    "{{.SourceAddress}}" = "<path to go bin directory>"
  }
  direct {}
}
{{.Tick}}{{.Tick}}{{.Tick}}

## Registry

This provider is prepared for publication to the Terraform Registry. The source
address {{.Tick}}{{.SourceAddress}}{{.Tick}} can be used once the provider is
published by the operator. The generated release workflow and
{{.Tick}}.goreleaser.yml{{.Tick}} produce Terraform Registry-compatible artifacts,
but the repository itself does **not** automatically submit or list the provider
on registry.terraform.io. Registry publishing is left to the operator.
`

// RegistryManifest returns the generated terraform-registry-manifest.json
// file for the provider.
func RegistryManifest(cfg BuildConfig) File {
	return Template("terraform-registry-manifest.json", registryManifestTemplate, map[string]any{
		"ProtocolVersions": cfg.protocolVersions(),
	})
}

const registryManifestTemplate = `{
  "version": 1,
  "metadata": {
    "protocol_versions": [{{range $i, $v := .ProtocolVersions}}{{if $i}}, {{end}}"{{$v}}"{{end}}]
  }
}
`
