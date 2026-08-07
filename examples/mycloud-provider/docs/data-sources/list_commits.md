---
page_title: "mycloud_list_commits Data Source - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_list_commits Data Source



## Example Usage

```terraform
data "mycloud_list_commits" "example" {
  organization = null
  project = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)
* `project` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (List(Object({author_name, committed_at, message, organization, project, ref, sha})), computed)

