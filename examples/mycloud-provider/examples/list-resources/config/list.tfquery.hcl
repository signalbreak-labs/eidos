list "mycloud_config" "example" {
  provider = mycloud
  limit    = 100
  config {
    workspace = "example"
  }
}
