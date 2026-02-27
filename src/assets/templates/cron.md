---
title: "{{title}}"
type: {{extra.type}}
status: {{extra.status}}
tags:
  - cron{{extra.tags}}
created: {{format-date now}}
{{#if extra.priority}}priority: {{extra.priority}}
{{/if}}{{#if extra.projectId}}projectId: "{{extra.projectId}}"
{{/if}}{{#if extra.schedule}}schedule: "{{extra.schedule}}"
{{/if}}{{#if extra.next_run}}next_run: "{{extra.next_run}}"
{{/if}}{{#if extra.max_runs}}max_runs: {{extra.max_runs}}
{{/if}}{{#if extra.starts_at}}starts_at: "{{extra.starts_at}}"
{{/if}}{{#if extra.expires_at}}expires_at: "{{extra.expires_at}}"
{{/if}}---

{{content}}
