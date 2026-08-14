---
page_title: "mycloud_update_pull_request Action - mycloud"
subcategory: ""
description: |-
  Update a pull request
---

# mycloud_update_pull_request Action

Update a pull request

## Example Usage

```terraform
action "mycloud_update_pull_request" "example" {
  body = "example"
  html_url = "example"
  id = 1
  merged = true
  number = 1
  organization = "example"
  project = "example"
  pull_number = 1
  state = "example"
  title = "example"
}

```

## Schema

### Arguments

The following arguments are supported:

* `body` (String, optional)
* `html_url` (String, optional)
* `id` (Number, optional)
* `merged` (Bool, optional)
* `number` (Number, optional)
* `organization` (String, required)
* `project` (String, required)
* `pull_number` (Number, required)
* `state` (String, optional)
* `title` (String, optional)
