---
page_title: "mycloud_network Resource - mycloud"
subcategory: ""
description: |-
  Read a Network
---

# mycloud_network Resource

Read a Network

## Example Usage

```terraform
resource "mycloud_network" "example" {
  api_version = "example"
  kind        = "example"
  name        = "example"
  spec = {
    ip_address = "example"
    ports = [{
      name     = "example"
      port     = 0
      protocol = "example"
    }]
    selector = {
      "selector" = "example"
    }
  }
  status = {
    load_balancer = "example"
  }
  workspace = "example"
}
```

## Schema

### Arguments

The following arguments are supported:

* `api_version` (String, optional)
* `kind` (String, optional)
* `name` (String, required)
* `spec` (Attributes, optional) (see [below for nested schema](#nestedatt--spec))
* `status` (Attributes, optional) (see [below for nested schema](#nestedatt--status))
* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following computed attributes are exported:

* `api_version` (String, computed)
* `id` (String, computed)
* `kind` (String, computed)
* `spec` (Attributes, computed) (see [below for nested schema](#nestedatt--spec))
* `status` (Attributes, computed) (see [below for nested schema](#nestedatt--status))

<a id="nestedatt--spec"></a>
### Nested Schema for `spec`

Optional:

* `ip_address` (String)
* `ports` (Attributes List) (see [below for nested schema](#nestedatt--spec--ports))
* `selector` (Map of String)
<a id="nestedatt--spec--ports"></a>
### Nested Schema for `spec.ports`

Optional:

* `name` (String)
* `port` (Number)
* `protocol` (String)
<a id="nestedatt--status"></a>
### Nested Schema for `status`

Optional:

* `load_balancer` (Dynamic)

## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_network.example {workspace}:{name}
```
