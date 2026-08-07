---
page_title: "mycloud_list_secrets List Resource - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_list_secrets List Resource



## Example Usage

```terraform
list "mycloud_list_secrets" "example" {
  provider = mycloud
  limit = 100
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
