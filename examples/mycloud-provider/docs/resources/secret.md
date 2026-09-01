---
page_title: "mycloud_secret Resource - mycloud"
subcategory: ""
description: |-
  Create a Secret
---

# mycloud_secret Resource

Create a Secret

## Example Usage

```terraform
resource "mycloud_secret" "example" {
  api_version = "example"
  data = {
    "data" = "example"
  }
  kind      = "example"
  name      = "example"
  type      = "example"
  workspace = "example"
}
```

## Schema

### Arguments

The following arguments are supported:

* `api_version` (String, optional)
* `data` (Map of String, optional)
* `kind` (String, optional)
* `name` (String, required)
* `type` (String, optional)
* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following computed attributes are exported:

* `id` (String, computed)


## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_secret.example {workspace}:{name}
```
