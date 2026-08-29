---
page_title: "mycloud_secret Resource - mycloud"
subcategory: ""
description: |-
  Read a Secret
---

# mycloud_secret Resource

Read a Secret

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

* `api_version` (String, computed)
* `data` (Map of String, computed)
* `id` (String, computed)
* `kind` (String, computed)
* `type` (String, computed)


## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_secret.example {workspace}:{name}
```
