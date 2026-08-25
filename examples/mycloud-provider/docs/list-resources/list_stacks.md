---
page_title: "mycloud_list_stacks List Resource - mycloud"
subcategory: ""
description: |-
  List Stacks
---

# mycloud_list_stacks List Resource

List Stacks

## Example Usage

```terraform
list "mycloud_list_stacks" "example" {
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


