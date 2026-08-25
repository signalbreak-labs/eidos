---
page_title: "mycloud_list_commits Data Source - mycloud"
subcategory: ""
description: |-
  List commits
---

# mycloud_list_commits Data Source

List commits

## Example Usage

```terraform
data "mycloud_list_commits" "example" {
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

* `author_name` (String)
* `committed_at` (String)
* `message` (String)
* `organization` (String)
* `project` (String)
* `ref` (String)
* `sha` (String)

