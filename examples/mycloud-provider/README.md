# mycloud Terraform Provider

The `mycloud` Terraform provider is used to manage resources on `registry.terraform.io/mycloud/mycloud`.

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.26

## Development

Build and install the provider locally:

```shell
make install
```

To test the provider without publishing it, add a `dev_overrides` block to `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/mycloud/mycloud" = "<path to go bin directory>"
  }
  direct {}
}
```

## Registry

This provider is prepared for publication to the Terraform Registry. The source
address `registry.terraform.io/mycloud/mycloud` can be used once the provider is
published by the operator. The generated release workflow and
`.goreleaser.yml` produce Terraform Registry-compatible artifacts,
but the repository itself does **not** automatically submit or list the provider
on registry.terraform.io. Registry publishing is left to the operator.
