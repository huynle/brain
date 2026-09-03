package frontmatter

// ComposeEntryTags returns the tag list a created entry actually persists:
// each raw tag sanitized (invalid ones dropped), deduplicated in first-seen
// order, with the entry type appended when not already present.
//
// This exists so callers that need the persisted tags — notably the
// entry.created event emitter — do not have to re-derive them. Emitting raw
// request tags instead would advertise tags the stored file does not carry
// and omit the auto-appended type, so a "tags" event filter would silently
// disagree with the entry on disk.
//
// Generate applies the same dedup-and-append rules, so passing an already
// composed list through GenerateOptions.Tags is idempotent.
func ComposeEntryTags(raw []string, entryType string) []string {
	tags := make([]string, 0, len(raw)+1)
	seen := make(map[string]bool, len(raw)+1)
	for _, tag := range raw {
		sanitized, ok := SanitizeTag(tag)
		if !ok || seen[sanitized] {
			continue
		}
		seen[sanitized] = true
		tags = append(tags, sanitized)
	}
	if entryType != "" && !seen[entryType] {
		tags = append(tags, entryType)
	}
	return tags
}
