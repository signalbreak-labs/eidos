---
page_title: "mycloud_stack Resource - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_stack Resource



## Example Usage

```terraform
resource "mycloud_stack" "example" {
  api_version = null
  kind = null
  name = null
  spec = {}
  status = {}
  workspace = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `api_version` (String, optional)
* `kind` (String, optional)
* `name` (String, required)
* `spec` (Object({replicas, selector}), optional)
  * `replicas` (Number, computed)
  * `selector` (Map(String), computed)
* `status` (Object({ready_replicas}), optional)
  * `ready_replicas` (Number, computed)
* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following computed attributes are exported:

* `api_version` (String, computed)
* `id` (String, computed)
* `kind` (String, computed)
* `spec` (Object({replicas, selector}), computed)
  * `replicas` (Number, computed)
  * `selector` (Map(String), computed)
* `status` (Object({ready_replicas}), computed)
  * `ready_replicas` (Number, computed)

## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_stack.example {workspace}:{name}
```
