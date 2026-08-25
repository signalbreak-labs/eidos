---
page_title: "mycloud_create_pull_request Action - mycloud"
subcategory: ""
description: |-
  Create a pull request
---

# mycloud_create_pull_request Action

Create a pull request

## Example Usage

```terraform
action "mycloud_create_pull_request" "example" {
  config {
    body              = "example"
    body_organization = "example"
    body_project      = "example"
    html_url          = "example"
    id                = 1
    merged            = true
    number            = 1
    organization      = "example"
    project           = "example"
    pull_number       = 1
    state             = "example"
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
* `merged` (Boolean, optional)
* `number` (Number, optional)
* `organization` (String, required)
* `project` (String, required)
* `pull_number` (Number, optional)
* `state` (String, optional)
* `title` (String, optional)


