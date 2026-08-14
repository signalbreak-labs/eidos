---
page_title: "mycloud_list_pull_requests List Resource - mycloud"
subcategory: ""
description: |-
  List pull requests
---

# mycloud_list_pull_requests List Resource

List pull requests

## Example Usage

```terraform
list "mycloud_list_pull_requests" "example" {
  provider = mycloud
  limit = 100
  config {
    organization = "example"
    project = "example"
  }
}

```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)
* `project` (String, required)

### Identity Attributes

The following identity attributes are exported for each matching result:

* `organization` (String, computed)
* `project` (String, computed)
* `pull_number` (Number, computed)
