list "mycloud_secret" "example" {
  provider = mycloud
  limit    = 100
  config {
    workspace = "example"
  }
}
