---
page_title: "mycloud_stack Resource - mycloud"
subcategory: ""
description: |-
  Read a Stack
---

# mycloud_stack Resource

Read a Stack

## Example Usage

```terraform
resource "mycloud_stack" "example" {
  api_version = null
  kind        = null
  name        = null
  spec        = {}
  status      = {}
  workspace   = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `api_version` (String, optional)
* `kind` (String, optional)
* `name` (String, required)
* `spec` (Attributes, optional) (see [below for nested schema](#nestedatt--spec))
* `status` (Attributes, optional) (see [below for nested schema](#nestedatt--status))
* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following computed attributes are exported:

* `api_version` (String, computed)
* `id` (String, computed)
* `kind` (String, computed)
* `spec` (Attributes, computed) (see [below for nested schema](#nestedatt--spec))
* `status` (Attributes, computed) (see [below for nested schema](#nestedatt--status))

<a id="nestedatt--spec"></a>
### Nested Schema for `spec`

Optional:

* `replicas` (Number)
* `selector` (Map of String)
<a id="nestedatt--status"></a>
### Nested Schema for `status`

Optional:

* `ready_replicas` (Number)

## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_stack.example {workspace}:{name}
```
