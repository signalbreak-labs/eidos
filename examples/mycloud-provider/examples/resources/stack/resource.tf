resource "mycloud_stack" "example" {
  api_version = "example"
  kind        = "example"
  name        = "example"
  spec = {
    replicas = 0
    selector = {
      "selector" = "example"
    }
  }
  status = {
    ready_replicas = 0
  }
  workspace = "example"
}
