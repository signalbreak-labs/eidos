---
page_title: "mycloud_list_workspaces Data Source - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_list_workspaces Data Source



## Example Usage

```terraform
data "mycloud_list_workspaces" "example" {
}
```

## Schema

### Arguments

The following arguments are supported:


### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (List(Object({api_version, kind, labels, name, status})), computed)

