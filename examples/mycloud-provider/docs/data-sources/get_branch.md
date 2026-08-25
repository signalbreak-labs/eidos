---
page_title: "mycloud_get_branch Data Source - mycloud"
subcategory: ""
description: |-
  Get a branch
---

# mycloud_get_branch Data Source

Get a branch

## Example Usage

```terraform
data "mycloud_get_branch" "example" {
  branch       = null
  organization = null
  project      = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `branch` (String, required)
* `organization` (String, required)
* `project` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `id` (Number, computed)
* `name` (String, computed)
* `protected` (Boolean, computed)
* `sha` (String, computed)


