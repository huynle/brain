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
	SortBy     string // "modified", "created", "priority", "title"
	SortOrder  string // "asc", "desc"
	Limit      int
	Offset     int
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
