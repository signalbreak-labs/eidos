list "mycloud_instance" "example" {
  provider = mycloud
  limit    = 100
  config {
    workspace = "example"
  }
}
