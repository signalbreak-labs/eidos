---
page_title: "mycloud_list_projects_for_organization Data Source - mycloud"
subcategory: ""
description: |-
  List organization projects
---

# mycloud_list_projects_for_organization Data Source

List organization projects

## Example Usage

```terraform
data "mycloud_list_projects_for_organization" "example" {
  organization = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (Attributes List, computed) (see [below for nested schema](#nestedatt--items))

<a id="nestedatt--items"></a>
### Nested Schema for `items`

Read-Only:

* `default_branch` (String)
* `description` (String)
* `full_name` (String)
* `html_url` (String)
* `id` (Number)
* `name` (String)
* `organization` (String)
* `private` (Boolean)
* `project` (String)

