---
page_title: "mycloud Provider"
subcategory: ""
description: |-
  Trimmed MyCloud API reference spec for golden file regression tests. The compute schemas keep path parameters (name, workspace) at the top level as a stable baseline; focused transformer and...
---

# mycloud Provider

Trimmed MyCloud API reference spec for golden file regression tests. The compute schemas keep path parameters (name, workspace) at the top level as a stable baseline; focused transformer and generated-lifecycle tests cover one-level nested identity promotion. The compute family (workspaces, instances, networks, stacks, configs, secrets) exercises workspace-scoped CRUD and list resources; the projects family (organizations, projects, tasks, pull requests, members, branches, commits) exercises organization-scoped data sources and list resources.

## Resources

- [mycloud_config](resources/config.md)
- [mycloud_instance](resources/instance.md)
- [mycloud_network](resources/network.md)
- [mycloud_project](resources/project.md)
- [mycloud_secret](resources/secret.md)
- [mycloud_stack](resources/stack.md)
- [mycloud_workspace](resources/workspace.md)

## Data Sources

- [mycloud_list_members](data-sources/list_members.md)
- [mycloud_get_member](data-sources/get_member.md)
- [mycloud_list_projects_for_organization](data-sources/list_projects_for_organization.md)
- [mycloud_list_branches](data-sources/list_branches.md)
- [mycloud_get_branch](data-sources/get_branch.md)
- [mycloud_list_commits](data-sources/list_commits.md)
- [mycloud_get_commit](data-sources/get_commit.md)
- [mycloud_list_pull_requests](data-sources/list_pull_requests.md)
- [mycloud_get_pull_request](data-sources/get_pull_request.md)
- [mycloud_list_tasks](data-sources/list_tasks.md)
- [mycloud_get_task](data-sources/get_task.md)
- [mycloud_list_workspaces](data-sources/list_workspaces.md)
- [mycloud_list_configs](data-sources/list_configs.md)
- [mycloud_list_instances](data-sources/list_instances.md)
- [mycloud_list_networks](data-sources/list_networks.md)
- [mycloud_list_secrets](data-sources/list_secrets.md)
- [mycloud_list_stacks](data-sources/list_stacks.md)

## Actions

- [mycloud_create_pull_request](actions/create_pull_request.md)
- [mycloud_update_pull_request](actions/update_pull_request.md)
- [mycloud_create_task](actions/create_task.md)
- [mycloud_update_task](actions/update_task.md)

## List Resources

- [mycloud_project](list-resources/project.md)
- [mycloud_workspace](list-resources/workspace.md)
- [mycloud_config](list-resources/config.md)
- [mycloud_instance](list-resources/instance.md)
- [mycloud_network](list-resources/network.md)
- [mycloud_secret](list-resources/secret.md)
- [mycloud_stack](list-resources/stack.md)

