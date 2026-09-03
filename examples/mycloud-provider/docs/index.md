---
page_title: "mycloud Provider"
subcategory: ""
description: |-
  Trimmed MyCloud API reference spec for golden file regression tests. The compute schemas keep path parameters (name, workspace) at the top level as a stable baseline; focused transformer and...
---

# mycloud Provider

Trimmed MyCloud API reference spec for golden file regression tests. The compute schemas keep path parameters (name, workspace) at the top level as a stable baseline; focused transformer and generated-lifecycle tests cover one-level nested identity promotion. The compute family (workspaces, instances, networks, stacks, configs, secrets) exercises workspace-scoped CRUD and list resources; the projects family (organizations, projects, tasks, pull requests, members, branches, commits) exercises organization-scoped data sources and list resources.

## Example Usage

```terraform
provider "mycloud" {
  endpoint     = "example"
  bearer_token = "example"
}
```

## Schema

### Arguments

The following arguments are supported:

* `endpoint` (String, optional) - Overrides the default API base URL derived from the OpenAPI servers. Useful for directing the provider at a test or mock server.
* `bearer_token` (String, optional) - Bearer token used for HTTP bearer authentication.
* `log_file` (String, optional) - Path to a file that receives HTTP request/response trace logs. When unset, trace logging is disabled.
* `log_capture_request_headers` (Boolean, optional) - Capture request headers in the trace log. Sensitive headers are redacted.
* `log_capture_request_body` (Boolean, optional) - Capture request bodies in the trace log. Disabled by default to avoid writing sensitive payloads to disk.
* `log_capture_response_headers` (Boolean, optional) - Capture response headers in the trace log. Sensitive headers are redacted.
* `log_capture_response_body` (Boolean, optional) - Capture response bodies in the trace log. Disabled by default to avoid writing sensitive payloads to disk.
* `log_max_body_bytes` (Number, optional) - Maximum number of body bytes captured per log entry before truncation. Defaults to 4096.
* `tls_skip_verify` (Boolean, optional) - Disable TLS certificate verification for API requests. Defaults to false; enable only against endpoints with self-signed or otherwise untrusted certificates.


