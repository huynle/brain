---
title: {{title}}
type: {{extra.type}}
status: {{extra.status}}
tags:
  - task{{extra.tags}}
created: {{format-date now}}
{{#if extra.priority}}priority: {{extra.priority}}
{{/if}}{{#if extra.parent_id}}parent_id: {{extra.parent_id}}
{{/if}}{{#if extra.projectId}}projectId: {{extra.projectId}}
{{/if}}{{#if extra.depends_on}}depends_on: {{extra.depends_on}}
{{/if}}{{#if extra.workdir}}workdir: {{extra.workdir}}
{{/if}}{{#if extra.worktree}}worktree: {{extra.worktree}}
{{/if}}{{#if extra.git_remote}}git_remote: {{extra.git_remote}}
{{/if}}{{#if extra.git_branch}}git_branch: {{extra.git_branch}}
{{/if}}---

{{content}}
