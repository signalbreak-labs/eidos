---
page_title: "mycloud_get_commit Data Source - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_get_commit Data Source



## Example Usage

```terraform
data "mycloud_get_commit" "example" {
  organization = null
  project = null
  ref = null
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

