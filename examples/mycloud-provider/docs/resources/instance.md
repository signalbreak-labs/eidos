---
page_title: "mycloud_instance Resource - mycloud"
subcategory: ""
description: |-
  Read an Instance
---

# mycloud_instance Resource

Read an Instance

## Example Usage

```terraform
resource "mycloud_instance" "example" {
  api_version = null
  kind        = null
  labels      = {}
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
* `labels` (Map of String, optional)
* `name` (String, required)
* `spec` (Attributes, optional) (see [below for nested schema](#nestedatt--spec))
* `status` (Attributes, optional) (see [below for nested schema](#nestedatt--status))
* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following computed attributes are exported:

* `api_version` (String, computed)
* `id` (String, computed)
* `kind` (String, computed)
* `labels` (Map of String, computed)
* `spec` (Attributes, computed) (see [below for nested schema](#nestedatt--spec))
* `status` (Attributes, computed) (see [below for nested schema](#nestedatt--status))

<a id="nestedatt--spec"></a>
### Nested Schema for `spec`

Optional:

* `containers` (Attributes List) (see [below for nested schema](#nestedatt--spec--containers))
<a id="nestedatt--spec--containers"></a>
### Nested Schema for `spec.containers`

Required:

* `name` (String)
Optional:

* `image` (String)
* `image_pull_policy` (String)
<a id="nestedatt--status"></a>
### Nested Schema for `status`

Optional:

* `phase` (String)

## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_instance.example {workspace}:{name}
```
