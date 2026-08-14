---
page_title: "mycloud_list_secrets Data Source - mycloud"
subcategory: ""
description: |-
  List Secrets
---

# mycloud_list_secrets Data Source

List Secrets

## Example Usage

```terraform
data "mycloud_list_secrets" "example" {
  workspace = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (List(Object({api_version, data, kind, name, type, workspace})), computed)

