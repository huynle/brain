package supervision

// ToolCapabilities describes availability, not the caller's installed clients.
func ToolCapabilities() map[string]string {
	return map[string]string{
		"session_tail":             "control:*; OpenCode history requires connected runner",
		"session_children":         "control:*; persisted OpenCode linkage; live state unknown",
		"resource_health":          "read:*; updated runner and enabled memory guard required",
		"events_wait":              "read:*; retained ring; 25 second bound",
		"delivery_gate":            "read:*",
		"delivery_verify":          "admin:*; GitHub provider access required",
		"delivery_record":          "admin:*; exact artifact and revision required",
		"supervisor_capabilities":  "read:*; server registration, not client installation",
		"supervisor_snapshot":      "read:*; bounded non-atomic projection",
		"task_dispatch_preview":    "read:*; no reservation; runner-local configuration unknown",
		"supervisor_operation":     "admin:*; durable idempotency and optional budget/handoff",
		"supervisor_operation_get": "admin:*; submitting principal only",
		"supervisor_checkpoint":    "read:* for reads; admin:* for revision-checked mutations",
		"execution_budget":         "read:* for reads; admin:* for reservation protocol; opaque executor usage unknown",
	}
}
