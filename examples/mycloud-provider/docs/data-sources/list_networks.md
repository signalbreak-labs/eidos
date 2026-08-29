---
page_title: "mycloud_list_networks Data Source - mycloud"
subcategory: ""
description: |-
  List Networks
---

# mycloud_list_networks Data Source

List Networks

## Example Usage

```terraform
data "mycloud_list_networks" "example" {
  workspace = "example"
}
```

## Schema

### Arguments

The following arguments are supported:

* `workspace` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (Attributes List, computed) (see [below for nested schema](#nestedatt--items))

<a id="nestedatt--items"></a>
### Nested Schema for `items`

Read-Only:

* `api_version` (String)
* `kind` (String)
* `name` (String)
* `spec` (Attributes) (see [below for nested schema](#nestedatt--items--spec))
* `status` (Attributes) (see [below for nested schema](#nestedatt--items--status))
* `workspace` (String)

<a id="nestedatt--items--spec"></a>
### Nested Schema for `items.spec`

Read-Only:

* `ip_address` (String)
* `ports` (Attributes List) (see [below for nested schema](#nestedatt--items--spec--ports))
* `selector` (Map of String)

<a id="nestedatt--items--spec--ports"></a>
### Nested Schema for `items.spec.ports`

Read-Only:

* `name` (String)
* `port` (Number)
* `protocol` (String)

<a id="nestedatt--items--status"></a>
### Nested Schema for `items.status`

Read-Only:

* `load_balancer` (Dynamic)

