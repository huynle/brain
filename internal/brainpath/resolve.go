// Package brainpath validates root-relative filesystem paths. It does not open
// files or prevent TOCTOU races: callers must trust the filesystem not to change
// between validation and use. It is not an openat-style security boundary.
package brainpath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrContainment identifies invalid relative names and canonical paths outside
// the root (or naming the root itself). Use errors.Is to recognize it. Other
// filesystem errors retain their original identity through wrapping.
var ErrContainment = errors.New("path is not contained in root")

// Resolve validates an existing file or directory beneath root. Both root and
// candidate are canonicalized for comparison, including root aliases such as
// macOS /var -> /private/var. The returned path is absolute and lexically clean,
// NOT symlink-expanded: removing a final symlink removes the link, not its target.
// Empty names, NULs, absolute names, all '..' components, and the root itself are
// rejected. In-root symlinks are allowed; each path prefix is checked as well as
// the final target. Root is trusted configuration and must be a directory.
func Resolve(root, name string) (string, error) {
	return resolve(root, name, false)
}

// ResolveForWrite has Resolve's lexical-return and containment contract, but
// permits missing nested paths, including a missing root. Existing ancestors
// must be directories. Missing suffixes are appended to canonical existing
// ancestors; dangling symlinks are resolved, never treated as absent links.
// A dangling in-root target is allowed, an outside target is not. No directories
// or files are created. Callers must handle ordinary I/O errors after resolution.
// Dangling link targets containing '..' are conservatively rejected: lexical
// cleaning before resolving such a target could change its filesystem meaning.
func ResolveForWrite(root, name string) (string, error) {
	return resolve(root, name, true)
}

func resolve(root, name string, missing bool) (string, error) {
	if root == "" || strings.ContainsRune(root, 0) || name == "" || strings.ContainsRune(name, 0) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", fmt.Errorf("%w: invalid root or relative name %q", ErrContainment, name)
	}
	for _, part := range strings.Split(name, string(filepath.Separator)) {
		if part == ".." {
			return "", fmt.Errorf("%w: parent component in %q", ErrContainment, name)
		}
	}
	name = filepath.Clean(name)
	if name == "." {
		return "", fmt.Errorf("%w: name denotes root", ErrContainment)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	canonicalRoot, err := canonical(absRoot, missing, 0)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	if err := directory(canonicalRoot, missing); err != nil {
		return "", err
	}
	current := canonicalRoot
	parts := strings.Split(name, string(filepath.Separator))
	for i, part := range parts {
		current, err = canonical(filepath.Join(current, part), missing, 0)
		if err != nil {
			return "", fmt.Errorf("resolve %q: %w", name, err)
		}
		rel, err := filepath.Rel(canonicalRoot, current)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) || (rel == "." && i == len(parts)-1) {
			return "", fmt.Errorf("%w: %q", ErrContainment, name)
		}
		if i < len(parts)-1 {
			if err := directory(current, missing); err != nil {
				return "", err
			}
		}
	}
	return filepath.Join(absRoot, name), nil
}

func directory(path string, missing bool) error {
	info, err := os.Stat(path)
	if missing && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return &os.PathError{Op: "resolve", Path: path, Err: syscall.ENOTDIR}
	}
	return nil
}

// canonical uses the standard library for existing paths. For a missing path,
// Lstat distinguishes an absent name from a dangling link, then recursion
// canonicalizes its parent or link target. Only ENOENT permits missing suffixes;
// permission errors, non-directory ancestors and symlink loops fail closed.
func canonical(path string, missing bool, links int) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !missing || !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	info, statErr := os.Lstat(path)
	if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		if links >= 40 {
			return "", &os.PathError{Op: "resolve", Path: path, Err: syscall.ELOOP}
		}
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		for _, part := range strings.Split(target, string(filepath.Separator)) {
			if part == ".." {
				return "", fmt.Errorf("%w: ambiguous dangling link %q", ErrContainment, path)
			}
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		return canonical(target, missing, links+1)
	}
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolvedParent, err := canonical(parent, missing, links)
	if err != nil {
		return "", err
	}
	if err := directory(resolvedParent, missing); err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}
