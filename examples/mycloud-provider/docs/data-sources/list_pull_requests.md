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
  organization = null
  project = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)
* `project` (String, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `items` (List(Object({body, html_url, id, merged, number, organization, project, pull_number, state, title})), computed)

