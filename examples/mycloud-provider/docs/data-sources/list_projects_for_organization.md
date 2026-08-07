---
page_title: "mycloud_list_projects_for_organization Data Source - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_list_projects_for_organization Data Source



## Example Usage

```terraform
data "mycloud_list_projects_for_organization" "example" {
  organization = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (List(Object({default_branch, description, full_name, html_url, id, name, organization, private, project})), computed)

