---
page_title: "mycloud_list_branches Data Source - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_list_branches Data Source



## Example Usage

```terraform
data "mycloud_list_branches" "example" {
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

* `items` (List(Object({branch, id, name, organization, project, protected, sha})), computed)

