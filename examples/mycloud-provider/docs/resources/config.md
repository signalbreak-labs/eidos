---
page_title: "mycloud_config Resource - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_config Resource



## Example Usage

```terraform
resource "mycloud_config" "example" {
  api_version = null
  data = {}
  kind = null
  name = null
  workspace = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `api_version` (String, optional)
* `data` (Map(String), optional)
* `kind` (String, optional)
* `name` (String, required)
* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following computed attributes are exported:

* `api_version` (String, computed)
* `data` (Map(String), computed)
* `id` (String, computed)
* `kind` (String, computed)

## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_config.example {workspace}:{name}
```
