---
page_title: "mycloud_workspace Resource - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_workspace Resource



## Example Usage

```terraform
resource "mycloud_workspace" "example" {
  api_version = null
  kind = null
  labels = {}
  name = null
  status = {}
}
```

## Schema

### Arguments

The following arguments are supported:

* `api_version` (String, optional)
* `kind` (String, optional)
* `labels` (Map(String), optional)
* `name` (String, required)
* `status` (Object({phase}), optional)
  * `phase` (String, computed)

### Attributes

In addition to all arguments above, the following computed attributes are exported:

* `api_version` (String, computed)
* `kind` (String, computed)
* `labels` (Map(String), computed)
* `status` (Object({phase}), computed)
  * `phase` (String, computed)

## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_workspace.example {name}
```
