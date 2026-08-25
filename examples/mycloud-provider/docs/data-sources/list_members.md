---
page_title: "mycloud_list_members Data Source - mycloud"
subcategory: ""
description: |-
  List members
---

# mycloud_list_members Data Source

List members

## Example Usage

```terraform
data "mycloud_list_members" "example" {
}
```

## Schema

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (Attributes List, computed) (see [below for nested schema](#nestedatt--items))

<a id="nestedatt--items"></a>
### Nested Schema for `items`

Read-Only:

* `avatar_url` (String)
* `handle` (String)
* `html_url` (String)
* `id` (Number)
* `member` (String)
* `name` (String)

