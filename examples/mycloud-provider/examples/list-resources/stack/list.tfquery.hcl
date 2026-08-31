list "mycloud_stack" "example" {
  provider = mycloud
  limit    = 100
  config {
    workspace = "example"
  }
}
