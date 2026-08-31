---
page_title: "mycloud_create_pull_request Action - mycloud"
subcategory: ""
description: |-
  Create a pull request
---

# mycloud_create_pull_request Action

Create a pull request

-> **Note:** This action requires Terraform 1.14 or later. Standalone actions are invoked with `terraform apply -invoke=action.<type>.<name>` (or attached to a resource lifecycle `action_trigger`); a plain `terraform apply` does not invoke a standalone action block.

## Example Usage

```terraform
action "mycloud_create_pull_request" "example" {
  config {
    body              = "example"
    body_organization = "example"
    body_project      = "example"
    html_url          = "example"
    id                = 0
    merged            = true
    number            = 0
    organization      = "example"
    project           = "example"
    pull_number       = 0
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


