---
page_title: "mycloud_create_task Action - mycloud"
subcategory: ""
description: |-
  Create an task
---

# mycloud_create_task Action

Create an task

## Example Usage

```terraform
action "mycloud_create_task" "example" {
  config {
    body              = "example"
    body_organization = "example"
    body_project      = "example"
    html_url          = "example"
    id                = 1
    number            = 1
    organization      = "example"
    project           = "example"
    state             = "example"
    task_number       = 1
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
* `html_url` (String, optional)
* `id` (Number, optional)
* `number` (Number, optional)
* `organization` (String, required)
* `project` (String, required)
* `state` (String, optional)
* `task_number` (Number, optional)
* `title` (String, optional)


