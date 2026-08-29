---
page_title: "mycloud_list_tasks Data Source - mycloud"
subcategory: ""
description: |-
  List project tasks
---

# mycloud_list_tasks Data Source

List project tasks

## Example Usage

```terraform
data "mycloud_list_tasks" "example" {
  organization = "example"
  project      = "example"
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

* `body` (String)
* `html_url` (String)
* `id` (Number)
* `number` (Number)
* `organization` (String)
* `project` (String)
* `state` (String)
* `task_number` (Number)
* `title` (String)

