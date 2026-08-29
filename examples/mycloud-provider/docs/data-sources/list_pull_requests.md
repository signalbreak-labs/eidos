---
page_title: "mycloud_list_pull_requests Data Source - mycloud"
subcategory: ""
description: |-
  List pull requests
---

# mycloud_list_pull_requests Data Source

List pull requests

## Example Usage

```terraform
data "mycloud_list_pull_requests" "example" {
  organization = "example"
  project      = "example"
}
```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)
* `project` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (Attributes List, computed) (see [below for nested schema](#nestedatt--items))

<a id="nestedatt--items"></a>
### Nested Schema for `items`

Read-Only:

* `body` (String)
* `html_url` (String)
* `id` (Number)
* `merged` (Boolean)
* `number` (Number)
* `organization` (String)
* `project` (String)
* `pull_number` (Number)
* `state` (String)
* `title` (String)

