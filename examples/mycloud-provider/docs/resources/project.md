---
page_title: "mycloud_project Resource - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_project Resource



## Example Usage

```terraform
resource "mycloud_project" "example" {
  default_branch = null
  description = null
  full_name = null
  html_url = null
  id = null
  name = null
  organization = null
  private = null
  project = null
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
* `private` (Bool, optional)
* `project` (String, optional)

### Attributes

In addition to all arguments above, the following computed attributes are exported:

* `default_branch` (String, computed)
* `description` (String, computed)
* `full_name` (String, computed)
* `html_url` (String, computed)
* `id` (Number, computed)
* `name` (String, computed)
* `organization` (String, computed)
* `private` (Bool, computed)
* `project` (String, computed)

## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_project.example {organization}:{project}
```
