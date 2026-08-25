---
page_title: "mycloud_config Resource - mycloud"
subcategory: ""
description: |-
  Read a Config
---

# mycloud_config Resource

Read a Config

## Example Usage

```terraform
resource "mycloud_config" "example" {
  api_version = null
  data        = {}
  kind        = null
  name        = null
  workspace   = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `api_version` (String, optional)
* `data` (Map of String, optional)
* `kind` (String, optional)
* `name` (String, required)
* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following computed attributes are exported:

* `api_version` (String, computed)
* `data` (Map of String, computed)
* `id` (String, computed)
* `kind` (String, computed)


## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_config.example {workspace}:{name}
```
