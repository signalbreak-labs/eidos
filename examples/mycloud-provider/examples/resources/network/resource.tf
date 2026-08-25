resource "mycloud_network" "example" {
  api_version = "example"
  kind        = "example"
  name        = "example"
  spec = {
    ip_address = "example"
    ports = [{
      name     = "example"
      port     = 1
      protocol = "example"
    }]
    selector = {
      "selector" = "example"
    }
  }
  status = {
    load_balancer = null
  }
  workspace = "example"
}
