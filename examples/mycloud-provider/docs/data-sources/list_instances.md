---
page_title: "mycloud_list_instances Data Source - mycloud"
subcategory: ""
description: |-
  List Instances
---

# mycloud_list_instances Data Source

List Instances

## Example Usage

```terraform
data "mycloud_list_instances" "example" {
  workspace = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (Attributes List, computed) (see [below for nested schema](#nestedatt--items))

<a id="nestedatt--items"></a>
### Nested Schema for `items`

Read-Only:

* `api_version` (String)
* `kind` (String)
* `labels` (Map of String)
* `name` (String)
* `spec` (Attributes) (see [below for nested schema](#nestedatt--items--spec))
* `status` (Attributes) (see [below for nested schema](#nestedatt--items--status))
* `workspace` (String)
<a id="nestedatt--items--spec"></a>
### Nested Schema for `items.spec`

Read-Only:

* `containers` (Attributes List) (see [below for nested schema](#nestedatt--items--spec--containers))
<a id="nestedatt--items--spec--containers"></a>
### Nested Schema for `items.spec.containers`

Read-Only:

* `image` (String)
* `image_pull_policy` (String)
* `name` (String)
<a id="nestedatt--items--status"></a>
### Nested Schema for `items.status`

Read-Only:

* `phase` (String)

