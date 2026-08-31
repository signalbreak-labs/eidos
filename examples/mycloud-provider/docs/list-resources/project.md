---
page_title: "mycloud_project List Resource - mycloud"
subcategory: ""
description: |-
  List organization projects
---

# mycloud_project List Resource

List organization projects

-> **Note:** This list resource requires Terraform 1.14 or later and is used through the `terraform query` command, not in configuration files.

## Example Usage

```terraform
list "mycloud_project" "example" {
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


