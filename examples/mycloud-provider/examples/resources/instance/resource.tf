resource "mycloud_instance" "example" {
  api_version = "example"
  kind        = "example"
  labels = {
    "labels" = "example"
  }
  name = "example"
  spec = {
    containers = [{
      image             = "example"
      image_pull_policy = "example"
      name              = "example"
    }]
  }
  status = {
    phase = "example"
  }
  workspace = "example"
}
