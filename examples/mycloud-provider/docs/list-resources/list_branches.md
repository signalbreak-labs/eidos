---
page_title: "mycloud_list_branches List Resource - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_list_branches List Resource



## Example Usage

```terraform
list "mycloud_list_branches" "example" {
  provider = mycloud
  limit = 100
  config {
    organization = "example"
    project = "example"
  }
}

```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)
* `project` (String, required)

### Identity Attributes

The following identity attributes are exported for each matching result:

* `organization` (String, computed)
* `project` (String, computed)
* `branch` (String, computed)
