---
page_title: "mycloud_get_member Data Source - mycloud"
subcategory: ""
description: |-
  Get a member
---

# mycloud_get_member Data Source

Get a member

## Example Usage

```terraform
data "mycloud_get_member" "example" {
  member = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `member` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `avatar_url` (String, computed)
* `handle` (String, computed)
* `html_url` (String, computed)
* `id` (Number, computed)
* `name` (String, computed)


