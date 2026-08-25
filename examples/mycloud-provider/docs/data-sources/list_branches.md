---
page_title: "mycloud_list_branches Data Source - mycloud"
subcategory: ""
description: |-
  List branches
---

# mycloud_list_branches Data Source

List branches

## Example Usage

```terraform
data "mycloud_list_branches" "example" {
  organization = null
  project      = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)
* `project` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (Attributes List, computed) (see [below for nested schema](#nestedatt--items))

<a id="nestedatt--items"></a>
### Nested Schema for `items`

Read-Only:

* `branch` (String)
* `id` (Number)
* `name` (String)
* `organization` (String)
* `project` (String)
* `protected` (Boolean)
* `sha` (String)

