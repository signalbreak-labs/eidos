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
  organization = null
  project = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)
* `project` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (List(Object({body, html_url, id, number, organization, project, state, task_number, title})), computed)

