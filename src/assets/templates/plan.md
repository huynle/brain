---
title: {{title}}
type: {{extra.type}}
status: {{extra.status}}
tags:
  - plan{{extra.tags}}
created: {{format-date now}}
{{#if extra.priority}}priority: {{extra.priority}}
{{/if}}{{#if extra.projectId}}projectId: {{extra.projectId}}
{{/if}}---

{{content}}
