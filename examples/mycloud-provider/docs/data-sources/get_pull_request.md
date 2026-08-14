---
page_title: "mycloud_get_pull_request Data Source - mycloud"
subcategory: ""
description: |-
  Get a pull request
---

# mycloud_get_pull_request Data Source

Get a pull request

## Example Usage

```terraform
data "mycloud_get_pull_request" "example" {
  organization = null
  project = null
  pull_number = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)
* `project` (String, required)
* `pull_number` (Number, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `body` (String, computed)
* `html_url` (String, computed)
* `id` (Number, computed)
* `merged` (Bool, computed)
* `number` (Number, computed)
* `state` (String, computed)
* `title` (String, computed)

