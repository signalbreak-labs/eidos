---
page_title: "mycloud_list_workspaces List Resource - mycloud"
subcategory: ""
description: |-
  List Workspaces
---

# mycloud_list_workspaces List Resource

List Workspaces

## Example Usage

```terraform
list "mycloud_list_workspaces" "example" {
  provider = mycloud
  limit    = 100
}

```
## Schema

### Identity Attributes

The following identity attributes are exported for each matching result:

* `name` (String, computed)


