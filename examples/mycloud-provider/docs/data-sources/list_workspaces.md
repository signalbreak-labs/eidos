---
page_title: "mycloud_list_workspaces Data Source - mycloud"
subcategory: ""
description: |-
  List Workspaces
---

# mycloud_list_workspaces Data Source

List Workspaces

## Example Usage

```terraform
data "mycloud_list_workspaces" "example" {
}
```

## Schema

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
* `status` (Attributes) (see [below for nested schema](#nestedatt--items--status))
<a id="nestedatt--items--status"></a>
### Nested Schema for `items.status`

Read-Only:

* `phase` (String)

