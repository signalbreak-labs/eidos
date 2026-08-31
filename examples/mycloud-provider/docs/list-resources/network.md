---
page_title: "mycloud_network List Resource - mycloud"
subcategory: ""
description: |-
  List Networks
---

# mycloud_network List Resource

List Networks

-> **Note:** This list resource requires Terraform 1.14 or later and is used through the `terraform query` command, not in configuration files.

## Example Usage

```terraform
list "mycloud_network" "example" {
  provider = mycloud
  limit    = 100
  config {
    workspace = "example"
  }
}
```
## Schema

### Arguments

The following arguments are supported:

* `workspace` (String, required)


### Identity Attributes

The following identity attributes are exported for each matching result:

* `workspace` (String, computed)
* `name` (String, computed)


