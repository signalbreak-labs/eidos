---
page_title: "mycloud_list_projects_for_organization List Resource - mycloud"
subcategory: ""
description: |-
  List organization projects
---

# mycloud_list_projects_for_organization List Resource

List organization projects

## Example Usage

```terraform
list "mycloud_list_projects_for_organization" "example" {
  provider = mycloud
  limit    = 100
  config {
    organization = "example"
  }
}

```
## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)


### Identity Attributes

The following identity attributes are exported for each matching result:

* `organization` (String, computed)
* `project` (String, computed)


