//go:build !darwin

package indexer

// Other platforms retain fsnotify's existing root anchor/event delivery.
func watchRootChanges(string) (<-chan error, func(), error) {
	return nil, nil, nil
}
