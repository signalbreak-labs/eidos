---
page_title: "mycloud_update_task Action - mycloud"
subcategory: ""
description: |-
  Update an task
---

# mycloud_update_task Action

Update an task

## Example Usage

```terraform
action "mycloud_update_task" "example" {
  config {
    body              = "example"
    body_organization = "example"
    body_project      = "example"
    body_task_number  = 0
    html_url          = "example"
    id                = 0
    number            = 0
    organization      = "example"
    project           = "example"
    state             = "example"
    task_number       = 0
    title             = "example"
  }
}

```
## Schema

### Arguments

The following arguments are supported:

* `body` (String, optional)
* `body_organization` (String, optional)
* `body_project` (String, optional)
* `body_task_number` (Number, optional)
* `html_url` (String, optional)
* `id` (Number, optional)
* `number` (Number, optional)
* `organization` (String, required)
* `project` (String, required)
* `state` (String, optional)
* `task_number` (Number, required)
* `title` (String, optional)


