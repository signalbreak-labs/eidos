---
page_title: "mycloud_workspace Resource - mycloud"
subcategory: ""
description: |-
  Read a Workspace
---

# mycloud_workspace Resource

Read a Workspace

## Example Usage

```terraform
resource "mycloud_workspace" "example" {
  api_version = "example"
  kind        = "example"
  labels = {
    "labels" = "example"
  }
  name = "example"
  status = {
    phase = "example"
  }
}
```

## Schema

### Arguments

The following arguments are supported:

* `api_version` (String, optional)
* `kind` (String, optional)
* `labels` (Map of String, optional)
* `name` (String, required)
* `status` (Attributes, optional) (see [below for nested schema](#nestedatt--status))

<a id="nestedatt--status"></a>
### Nested Schema for `status`

Optional:

* `phase` (String)

## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_workspace.example {name}
```
