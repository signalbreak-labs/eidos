---
page_title: "mycloud_list_tasks List Resource - mycloud"
subcategory: ""
description: |-
  List project tasks
---

# mycloud_list_tasks List Resource

List project tasks

## Example Usage

```terraform
list "mycloud_list_tasks" "example" {
  provider = mycloud
  limit = 100
  config {
    organization = "example"
    project = "example"
  }
}

```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)
* `project` (String, required)

### Identity Attributes

The following identity attributes are exported for each matching result:

* `organization` (String, computed)
* `project` (String, computed)
* `task_number` (Number, computed)
