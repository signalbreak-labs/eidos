---
page_title: "mycloud_list_stacks Data Source - mycloud"
subcategory: ""
description: |-
  List Stacks
---

# mycloud_list_stacks Data Source

List Stacks

## Example Usage

```terraform
data "mycloud_list_stacks" "example" {
  workspace = "example"
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
* `name` (String)
* `spec` (Attributes) (see [below for nested schema](#nestedatt--items--spec))
* `status` (Attributes) (see [below for nested schema](#nestedatt--items--status))
* `workspace` (String)
<a id="nestedatt--items--spec"></a>
### Nested Schema for `items.spec`

Read-Only:

* `replicas` (Number)
* `selector` (Map of String)
<a id="nestedatt--items--status"></a>
### Nested Schema for `items.status`

Read-Only:

* `ready_replicas` (Number)

