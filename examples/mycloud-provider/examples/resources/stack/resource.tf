resource "mycloud_stack" "example" {
  api_version = "example"
  kind = "example"
  name = "example"
  spec = {
    replicas = 1
    selector = {
      "selector" = "example"
    }
  }
  status = {
    ready_replicas = 1
  }
  workspace = "example"
}
