---
page_title: "mycloud_list_configs List Resource - mycloud"
subcategory: ""
description: |-
  List Configs
---

# mycloud_list_configs List Resource

List Configs

## Example Usage

```terraform
list "mycloud_list_configs" "example" {
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


