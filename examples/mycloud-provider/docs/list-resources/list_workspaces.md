---
page_title: "mycloud_list_workspaces List Resource - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_list_workspaces List Resource



## Example Usage

```terraform
list "mycloud_list_workspaces" "example" {
  provider = mycloud
  limit = 100
}

```

## Schema

### Arguments

The following arguments are supported:


### Identity Attributes

The following identity attributes are exported for each matching result:

* `name` (String, computed)
