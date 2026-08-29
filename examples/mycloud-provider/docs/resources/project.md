---
page_title: "mycloud_project Resource - mycloud"
subcategory: ""
description: |-
  Get a project
---

# mycloud_project Resource

Get a project

## Example Usage

```terraform
resource "mycloud_project" "example" {
  default_branch = "example"
  description    = "example"
  full_name      = "example"
  html_url       = "example"
  id             = 0
  name           = "example"
  organization   = "example"
  private        = true
  project        = "example"
}
```

## Schema

### Arguments

The following arguments are supported:

* `default_branch` (String, optional)
* `description` (String, optional)
* `full_name` (String, optional)
* `html_url` (String, optional)
* `id` (Number, optional)
* `name` (String, optional)
* `organization` (String, optional)
* `private` (Boolean, optional)
* `project` (String, optional)


## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_project.example {organization}:{project}
```
