---
page_title: "mycloud_list_configs Data Source - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_list_configs Data Source



## Example Usage

```terraform
data "mycloud_list_configs" "example" {
  workspace = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (List(Object({api_version, data, kind, name, workspace})), computed)

