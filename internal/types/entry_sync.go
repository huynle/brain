package types

type EntryChange struct {
	Path    string      `json:"path"`
	Deleted bool        `json:"deleted,omitempty"`
	Entry   *BrainEntry `json:"entry,omitempty"`
	Raw     string      `json:"raw,omitempty"`
}
type EntryChanges struct {
	Epoch   string        `json:"epoch"`
	Cursor  int64         `json:"cursor"`
	More    bool          `json:"more"`
	Changes []EntryChange `json:"changes"`
}
