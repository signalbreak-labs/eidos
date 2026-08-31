list "mycloud_network" "example" {
  provider = mycloud
  limit    = 100
  config {
    workspace = "example"
  }
}
