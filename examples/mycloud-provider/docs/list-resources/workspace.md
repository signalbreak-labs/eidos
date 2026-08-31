---
page_title: "mycloud_workspace List Resource - mycloud"
subcategory: ""
description: |-
  List Workspaces
---

# mycloud_workspace List Resource

List Workspaces

-> **Note:** This list resource requires Terraform 1.14 or later and is used through the `terraform query` command, not in configuration files.

## Example Usage

```terraform
list "mycloud_workspace" "example" {
  provider = mycloud
  limit    = 100
}
```
## Schema

### Identity Attributes

The following identity attributes are exported for each matching result:

* `name` (String, computed)


