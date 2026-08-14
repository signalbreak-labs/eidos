---
page_title: "mycloud_instance Resource - mycloud"
subcategory: ""
description: |-
  Read an Instance
---

# mycloud_instance Resource

Read an Instance

## Example Usage

```terraform
resource "mycloud_instance" "example" {
  api_version = null
  kind = null
  labels = {}
  name = null
  spec = {}
  status = {}
  workspace = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `api_version` (String, optional)
* `kind` (String, optional)
* `labels` (Map(String), optional)
* `name` (String, required)
* `spec` (Object({containers}), optional)
  * `containers` (List(Object({image, image_pull_policy, name})), optional)
* `status` (Object({phase}), optional)
  * `phase` (String, optional)
* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following computed attributes are exported:

* `api_version` (String, computed)
* `id` (String, computed)
* `kind` (String, computed)
* `labels` (Map(String), computed)
* `spec` (Object({containers}), computed)
  * `containers` (List(Object({image, image_pull_policy, name})), optional)
* `status` (Object({phase}), computed)
  * `phase` (String, optional)

## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_instance.example {workspace}:{name}
```
