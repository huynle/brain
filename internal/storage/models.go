package storage

// NoteRow represents a row in the notes table.
type NoteRow struct {
	ID         int64
	Path       string
	ShortID    string
	Title      string
	Lead       *string // nullable
	Body       *string // nullable
	RawContent *string // nullable
	WordCount  int
	Checksum   *string // nullable
	Metadata   string  // JSON, defaults to "{}"
	Type       *string // nullable
	Status     *string // nullable
	Priority   *string // nullable
	ProjectID  *string // nullable
	FeatureID  *string // nullable
	Created    *string // nullable
	Modified   *string // nullable
	IndexedAt  string

	// MatchSource is populated by search queries to indicate whether the match
	// came from entry content or attachment-derived text. It is not stored in DB.
	MatchSource string
}

// LinkRow represents a row in the links table.
type LinkRow struct {
	ID         int64
	SourceID   int64
	TargetPath string
	TargetID   *int64 // nullable
	Title      string
	Href       string
	Type       string
	Snippet    string
}

// TagRow represents a row in the tags table.
type TagRow struct {
	ID     int64
	NoteID int64
	Tag    string
}

// LinkInput is the input for SetLinks — one link to insert.
type LinkInput struct {
	TargetPath string
	Title      string
	Href       string
	Type       string // defaults to "markdown" if empty
	Snippet    string
}

// AttachmentInput is the input for CreateAttachment.
type AttachmentInput struct {
	Digest    string
	Size      int64
	MediaType string
	Metadata  string // JSON, defaults to "{}" if empty
}

// AttachmentRow represents a row in the attachments table.
type AttachmentRow struct {
	ID        int64
	Digest    string
	Size      int64
	MediaType string
	Metadata  string // JSON
	CreatedAt string
}

// AttachmentDerivedInput is the input for UpsertAttachmentDerived.
type AttachmentDerivedInput struct {
	AttachmentID int64
	Kind         string
	Status       string
	ContentType  string
	Text         string
	Error        string
	Metadata     string // JSON, defaults to "{}" if empty
}

// AttachmentDerivedRow represents derived attachment extraction output/status.
type AttachmentDerivedRow struct {
	ID           int64
	AttachmentID int64
	Kind         string
	Status       string
	ContentType  string
	Text         string
	Error        string
	Metadata     string
	CreatedAt    string
	UpdatedAt    string
}

// EntryAttachmentRow represents a row in the entry_attachments table.
type EntryAttachmentRow struct {
	ID           int64
	NoteID       int64
	NotePath     string
	AttachmentID int64
	Role         string
	CreatedAt    string
}

// EntryMetaRow represents a row in the entry_meta table.
type EntryMetaRow struct {
	Path         string
	ProjectID    *string // nullable
	AccessCount  int
	LastAccessed *string // nullable
	LastVerified *string // nullable
	CreatedAt    string
}

// SearchOptions configures search behavior.
type SearchOptions struct {
	Strategy   string // "fts", "exact", "like" (default: "fts")
	Limit      int
	PathPrefix string
	Type       string
	Status     string
	ProjectID  string
	FeatureID  string
	Tags       []string
	Priority   string
}

// ListOptions configures list/filter behavior.
type ListOptions struct {
	Type       string
	Status     string
	ProjectID  string
	FeatureID  string
	PathPrefix string
	Tag        string
	Tags       []string
	Priority   string
	SortBy     string // "modified", "created", "priority", "title"
	SortOrder  string // "asc", "desc"
	Limit      int
	Offset     int
}

// EmbeddingSearchOptions configures embedding-based semantic search.
type EmbeddingSearchOptions struct {
	Limit     int
	ProjectID string
	Type      string
	Status    string
	FeatureID string
	Priority  string
	Tags      []string
}

// OrphanOptions configures the GetOrphans query.
type OrphanOptions struct {
	Type  string
	Limit int
}

// StaleOptions configures the GetStaleEntries query.
type StaleOptions struct {
	Type  string
	Limit int
}

// StatsOptions configures the GetStats query.
type StatsOptions struct {
	Path string // optional path prefix filter
}

// Stats holds aggregate storage statistics.
type Stats struct {
	TotalNotes   int
	ByType       map[string]int
	OrphanCount  int
	TrackedCount int
	StaleCount   int
}
