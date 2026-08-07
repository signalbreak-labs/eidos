---
page_title: "mycloud_network Resource - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_network Resource



## Example Usage

```terraform
resource "mycloud_network" "example" {
  api_version = null
  kind = null
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
* `name` (String, required)
* `spec` (Object({ip_address, ports, selector}), optional)
  * `ip_address` (String, computed)
  * `ports` (List(Object({name, port, protocol})), computed)
  * `selector` (Map(String), computed)
* `status` (Object({load_balancer}), optional)
  * `load_balancer` (Dynamic, computed)
* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following computed attributes are exported:

* `api_version` (String, computed)
* `id` (String, computed)
* `kind` (String, computed)
* `spec` (Object({ip_address, ports, selector}), computed)
  * `ip_address` (String, computed)
  * `ports` (List(Object({name, port, protocol})), computed)
  * `selector` (Map(String), computed)
* `status` (Object({load_balancer}), computed)
  * `load_balancer` (Dynamic, computed)

## Import

Import is supported using the following syntax:

```shell
terraform import mycloud_network.example {workspace}:{name}
```
