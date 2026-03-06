---
title: "{{title}}"
type: {{extra.type}}
status: {{extra.status}}
tags:
  - task{{extra.tags}}
created: {{format-date now}}
{{#if extra.priority}}priority: {{extra.priority}}
{{/if}}{{#if extra.projectId}}projectId: "{{extra.projectId}}"
{{/if}}{{#if extra.depends_on}}depends_on: {{extra.depends_on}}
{{/if}}{{#if extra.workdir}}workdir: "{{extra.workdir}}"
{{/if}}{{#if extra.git_remote}}git_remote: "{{extra.git_remote}}"
{{/if}}{{#if extra.git_branch}}git_branch: "{{extra.git_branch}}"
{{/if}}{{#if extra.user_original_request}}user_original_request: {{extra.user_original_request}}
{{/if}}{{#if extra.direct_prompt}}direct_prompt: {{extra.direct_prompt}}
{{/if}}{{#if extra.target_workdir}}target_workdir: "{{extra.target_workdir}}"
{{/if}}{{#if extra.feature_id}}feature_id: "{{extra.feature_id}}"
{{/if}}{{#if extra.feature_priority}}feature_priority: {{extra.feature_priority}}
{{/if}}{{#if extra.feature_depends_on}}feature_depends_on: {{extra.feature_depends_on}}
{{/if}}{{#if extra.agent}}agent: "{{extra.agent}}"
{{/if}}{{#if extra.model}}model: "{{extra.model}}"
{{/if}}{{#if extra.schedule}}schedule: "{{extra.schedule}}"
{{/if}}{{#if extra.schedule_enabled}}schedule_enabled: {{extra.schedule_enabled}}
{{/if}}{{#if extra.execution_mode}}execution_mode: {{extra.execution_mode}}
{{/if}}{{#if extra.merge_target_branch}}merge_target_branch: "{{extra.merge_target_branch}}"
{{/if}}{{#if extra.merge_policy}}merge_policy: {{extra.merge_policy}}
{{/if}}{{#if extra.merge_strategy}}merge_strategy: {{extra.merge_strategy}}
{{/if}}{{#if extra.remote_branch_policy}}remote_branch_policy: {{extra.remote_branch_policy}}
{{/if}}{{#if extra.open_pr_before_merge}}open_pr_before_merge: {{extra.open_pr_before_merge}}

{{/if}}{{#if extra.complete_on_idle}}complete_on_idle: {{extra.complete_on_idle}}
{{/if}}{{#if extra.generated}}generated: {{extra.generated}}
{{/if}}{{#if extra.generated_kind}}generated_kind: {{extra.generated_kind}}
{{/if}}{{#if extra.generated_key}}generated_key: "{{extra.generated_key}}"
{{/if}}{{#if extra.generated_by}}generated_by: "{{extra.generated_by}}"
{{/if}}---

{{content}}
