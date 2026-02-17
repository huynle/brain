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
{{/if}}---

{{content}}
