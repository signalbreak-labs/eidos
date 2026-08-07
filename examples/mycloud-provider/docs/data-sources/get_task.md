---
page_title: "mycloud_get_task Data Source - mycloud"
subcategory: ""
description: |-
  
---

# mycloud_get_task Data Source



## Example Usage

```terraform
data "mycloud_get_task" "example" {
  organization = null
  project = null
  task_number = null
}
```

## Schema

### Arguments

The following arguments are supported:

* `organization` (String, required)
* `project` (String, required)
* `task_number` (Number, required)

### Attributes

In addition to all arguments above, the following attributes are exported:

* `body` (String, computed)
* `html_url` (String, computed)
* `id` (Number, computed)
* `number` (Number, computed)
* `state` (String, computed)
* `title` (String, computed)

