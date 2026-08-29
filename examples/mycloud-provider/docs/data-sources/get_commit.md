---
page_title: "mycloud_get_commit Data Source - mycloud"
subcategory: ""
description: |-
  Get a commit
---

# mycloud_get_commit Data Source

Get a commit

## Example Usage

```terraform
data "mycloud_get_commit" "example" {
  organization = "example"
  project      = "example"
  ref          = "example"
}
```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)
* `project` (String, required)
* `ref` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `author_name` (String, computed)
* `committed_at` (String, computed)
* `message` (String, computed)
* `sha` (String, computed)


