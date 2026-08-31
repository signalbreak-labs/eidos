list "mycloud_project" "example" {
  provider = mycloud
  limit    = 100
  config {
    organization = "example"
  }
}
