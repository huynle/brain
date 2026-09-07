// Package tenantfs supplies tenant filesystem policy, not authentication or I/O.
// Provisioning must only be called by trusted bootstrap/operator composition.
// The filesystem and provisioning must be fenced against changes between a
// check and its use. This is NOT an openat boundary: TOCTOU remains possible.
package tenantfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/huynle/brain-api/internal/tenant"
)

type Layout string

const LegacyCAS Layout = "legacy"
const TenantCAS Layout = "tenant-sha256"

// Mapping is immutable durable configuration. Original strings preserve legacy
// configuration exactly. Absolute paths anchor relative config across restarts;
// canonical identities detect aliases and fail closed on filesystem root drift.
type Mapping struct {
	ID                            tenant.ID
	BrainRoot, BlobRoot           string
	BrainAbsolute, BlobAbsolute   string
	BrainCanonical, BlobCanonical string
	Layout                        Layout
}

// Repository must serialize validation with insertion against ALL registrations,
// including other processes. Identical registrations are idempotent; replacement
// is forbidden. A failed validator must leave no row. Callbacks must not reenter
// the repository. Lists are complete, authoritative snapshots (never paginated).
type Repository interface {
	ListTenantRoots(context.Context) ([]Mapping, error)
	RegisterTenantRoots(context.Context, Mapping, func([]Mapping) error) error
}
type Resolver struct {
	repo Repository
	base string
}
type Root struct {
	resolver *Resolver
	id       tenant.ID
	blobs    bool
}
type Overrides struct{ BrainRoot, BlobRoot string }

var ErrDenied = errors.New("tenant filesystem access denied")
var ErrConflict = errors.New("tenant root conflict")
var ErrUnknown = errors.New("unknown tenant")

// New neither provisions tenants nor creates directories. Database placement and
// tenancy mode are deliberately absent: trusted composition decides when to call
// ProvisionLocal or Provision, and passes the already-open shared repository.
func New(repo Repository, base string) (*Resolver, error) {
	if repo == nil {
		return nil, fmt.Errorf("nil tenant root repository")
	}
	abs, _, err := identity(base)
	if err != nil {
		return nil, err
	}
	return &Resolver{repo: repo, base: abs}, nil
}

// ProvisionLocal is the trusted, provisioning-free single-mode bootstrap seam.
// Repeating it checks exact configuration; promotion uses Lookup, not a new map.
func (r *Resolver) ProvisionLocal(ctx context.Context, brain, blob string) (Mapping, error) {
	// Validate persisted absolute anchors, not relative strings against today's
	// cwd. Exact original configuration is still required; root drift is checked
	// by snapshot just as it is for Lookup and repeated nonlocal provisioning.
	maps, err := r.snapshot(ctx)
	if err != nil {
		return Mapping{}, err
	}
	for _, m := range maps {
		if m.ID == tenant.Local {
			if brain != m.BrainRoot || blob != m.BlobRoot {
				return Mapping{}, ErrConflict
			}
			return m, nil
		}
	}
	return r.register(ctx, Mapping{ID: tenant.Local, BrainRoot: brain, BlobRoot: blob, Layout: LegacyCAS})
}

// Provision is an OPERATOR-ONLY registration seam, not a request resolver or an
// authorization check. Never expose it to tenant-controlled path configuration.
// New CAS roots are <brain-root>/blobs/<id>/sha256 unless explicitly overridden.
// The override is the CAS root itself, not a prefix to which IDs are appended.
func (r *Resolver) Provision(ctx context.Context, id tenant.ID, o Overrides) (Mapping, error) {
	if !id.Valid() || id == tenant.Local {
		return Mapping{}, ErrDenied
	}
	// An omitted override on a repeat must retain a previously explicit override.
	maps, err := r.snapshot(ctx)
	if err != nil {
		return Mapping{}, err
	}
	for _, m := range maps {
		if m.ID == id {
			if (o.BrainRoot != "" && o.BrainRoot != m.BrainRoot) || (o.BlobRoot != "" && o.BlobRoot != m.BlobRoot) {
				return Mapping{}, ErrConflict
			}
			return m, nil
		}
	}
	if o.BrainRoot == "" {
		o.BrainRoot = filepath.Join(r.base, "tenants", id.String())
	}
	if o.BlobRoot == "" {
		o.BlobRoot = filepath.Join(o.BrainRoot, "blobs", id.String(), "sha256")
	}
	return r.register(ctx, Mapping{ID: id, BrainRoot: o.BrainRoot, BlobRoot: o.BlobRoot, Layout: TenantCAS})
}

func (r *Resolver) register(ctx context.Context, m Mapping) (Mapping, error) {
	var err error
	m.BrainAbsolute, m.BrainCanonical, err = identity(m.BrainRoot)
	if err != nil {
		return Mapping{}, err
	}
	m.BlobAbsolute, m.BlobCanonical, err = identity(m.BlobRoot)
	if err != nil {
		return Mapping{}, err
	}
	err = r.repo.RegisterTenantRoots(ctx, m, func(existing []Mapping) error {
		for _, old := range existing {
			if old.ID == m.ID {
				if old != m {
					return ErrConflict
				}
				return validate(existing)
			}
		}
		return validate(append(existing, m))
	})
	if err != nil {
		return Mapping{}, err
	}
	return m, nil
}

func (r *Resolver) snapshot(ctx context.Context) ([]Mapping, error) {
	maps, err := r.repo.ListTenantRoots(ctx)
	if err != nil {
		return nil, err
	}
	if err = validate(maps); err != nil {
		return nil, err
	}
	return maps, nil
}

func (r *Resolver) Lookup(ctx context.Context, id tenant.ID) (Mapping, error) {
	if !id.Valid() {
		return Mapping{}, ErrDenied
	}
	maps, err := r.snapshot(ctx)
	if err != nil {
		return Mapping{}, err
	}
	for _, m := range maps {
		if m.ID == id {
			return m, nil
		}
	}
	return Mapping{}, ErrUnknown
}

func (r *Resolver) Brain(id tenant.ID) *Root { return &Root{resolver: r, id: id} }
func (r *Resolver) Blobs(id tenant.ID) *Root { return &Root{resolver: r, id: id, blobs: true} }

// Resolve returns a clean absolute lexical path to an existing non-root entry.
// All prefixes are admitted so a symlink through foreign storage cannot bounce
// back into owned storage. It never creates or provisions anything.
func (r *Root) Resolve(ctx context.Context, name string) (string, error) {
	return r.check(ctx, name, false, false, false)
}

// ResolveForWrite admits missing suffixes, but rejects dangling symlinks. Only
// ENOENT is treated as missing; loops, permissions and non-directories fail shut.
func (r *Root) ResolveForWrite(ctx context.Context, name string) (string, error) {
	return r.check(ctx, name, true, false, false)
}

// AdmitTraversal accepts "." for starting a walk. Invoke for EVERY entry before
// ingest/watch/read (including symlinks); prune rejected directories. An error
// must never be interpreted as permission. Ancestors of exclusions can be walked.
func (r *Root) AdmitTraversal(ctx context.Context, name string) error {
	_, err := r.check(ctx, name, false, true, false)
	return err
}

// PreflightDelete rejects ancestors of exclusions even when targets do not yet
// exist. Call before recursive removal; it does not authorize following symlinks
// during deletion or replace per-entry checks for custom recursive operations.
func (r *Root) PreflightDelete(ctx context.Context, name string) error {
	_, err := r.check(ctx, name, true, true, true)
	return err
}

func (r *Root) check(ctx context.Context, name string, missing, allowRoot, destructive bool) (string, error) {
	if !r.id.Valid() || name == "" || strings.ContainsRune(name, 0) || filepath.IsAbs(name) {
		return "", ErrDenied
	}
	for _, part := range strings.Split(name, string(filepath.Separator)) {
		if part == ".." {
			return "", ErrDenied
		}
	}
	name = filepath.Clean(name)
	if name == "." && !allowRoot {
		return "", ErrDenied
	}
	maps, err := r.resolver.snapshot(ctx)
	if err != nil {
		return "", err
	}
	var own Mapping
	for _, m := range maps {
		if m.ID == r.id {
			own = m
		}
	}
	if !own.ID.Valid() {
		return "", ErrUnknown
	}
	abs, canon := own.BrainAbsolute, own.BrainCanonical
	if r.blobs {
		abs, canon = own.BlobAbsolute, own.BlobCanonical
	}
	exclusions := []rootIdentity{}
	for _, m := range maps {
		if m.ID != own.ID {
			exclusions = append(exclusions, roots(m)...)
		}
	}
	// The legacy ancestor is an intentional exception, not a shared namespace.
	// Reserve even unregistered tenant names before a later provisioning occurs.
	if own.ID == tenant.Local {
		for _, root := range roots(own) {
			reserved := filepath.Join(root.abs, "tenants")
			canonical, err := canonical(reserved)
			if err != nil {
				return "", err
			}
			exclusions = append(exclusions, rootIdentity{reserved, canonical})
		}
	}
	current := abs
	parts := strings.Split(name, string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		links := 0
		if _, err := traceLinks(current, &links, func(path string) error {
			for _, foreign := range exclusions {
				if (within(foreign.abs, path) && !within(foreign.abs, abs)) ||
					(within(foreign.canon, path) && !within(foreign.canon, canon)) {
					return ErrDenied
				}
			}
			return nil
		}); err != nil {
			return "", err
		}
		resolved, err := canonical(current)
		if err != nil {
			return "", err
		}
		if !within(canon, resolved) || (!allowRoot && i == len(parts)-1 && resolved == canon) {
			return "", ErrDenied
		}
		for _, foreign := range exclusions {
			// A foreign ancestor (legacy local) does not exclude its child tenant.
			// Equal roots/other overlaps have already been rejected by validate.
			if (within(foreign.abs, current) && !within(foreign.abs, abs)) || (within(foreign.canon, resolved) && !within(foreign.canon, canon)) {
				return "", ErrDenied
			}
			if destructive && i == len(parts)-1 && (within(current, foreign.abs) || within(resolved, foreign.canon)) {
				return "", ErrDenied
			}
		}
		info, err := os.Stat(current)
		if err != nil {
			if !missing || !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		} else if i < len(parts)-1 && !info.IsDir() {
			return "", &os.PathError{Op: "resolve", Path: current, Err: syscall.ENOTDIR}
		}
	}
	return current, nil
}

func (r *Resolver) BlobPath(ctx context.Context, id tenant.ID, digest string) (string, error) {
	if len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		return "", ErrDenied
	}
	m, err := r.Lookup(ctx, id)
	if err != nil {
		return "", err
	}
	name := filepath.Join(digest[:2], digest)
	if m.Layout == LegacyCAS {
		name = filepath.Join(digest[:2], digest[2:4], digest)
	}
	return r.Blobs(id).ResolveForWrite(ctx, name)
}

type rootIdentity struct{ abs, canon string }

func roots(m Mapping) []rootIdentity {
	return []rootIdentity{{m.BrainAbsolute, m.BrainCanonical}, {m.BlobAbsolute, m.BlobCanonical}}
}
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func identity(path string) (string, string, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", "", ErrConflict
	}
	// Do not clean ambiguous symlink/.. configuration into a different directory.
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == ".." {
			return "", "", ErrConflict
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	c, err := canonical(abs)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(c)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	if err == nil && !info.IsDir() {
		return "", "", ErrConflict
	}
	return abs, c, nil
}

// canonical resolves BOTH sides, including /var -> /private/var on macOS.
// Missing suffixes are appended to a canonical existing directory. Lstat makes
// dangling symlinks an error, never an apparently safe absent path.
func canonical(path string) (string, error) {
	c, err := filepath.EvalSymlinks(path)
	if err == nil {
		return c, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, e := os.Lstat(path); e == nil {
		return "", err
	} else if !errors.Is(e, os.ErrNotExist) {
		return "", e
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	p, err := canonical(parent)
	if err != nil {
		return "", err
	}
	if info, e := os.Stat(p); e == nil {
		if !info.IsDir() {
			return "", &os.PathError{Op: "resolve", Path: p, Err: syscall.ENOTDIR}
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return "", e
	}
	return filepath.Join(p, filepath.Base(path)), nil
}

// traceLinks checks intermediate symlink targets that EvalSymlinks hides. Paths
// with parent components in link targets are conservatively refused: cleaning
// them before following earlier links would alter their filesystem meaning.
// Root configuration still uses EvalSymlinks; this stricter rule is for access.
func traceLinks(path string, links *int, visit func(string) error) (string, error) {
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(path, current), string(filepath.Separator))
	for i, part := range parts {
		if part == ".." {
			return "", ErrDenied
		}
		current = filepath.Join(current, part)
		if err := visit(current); err != nil {
			return "", err
		}
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			*links++
			if *links > 40 {
				return "", &os.PathError{Op: "resolve", Path: current, Err: syscall.ELOOP}
			}
			target, err := os.Readlink(current)
			if err != nil {
				return "", err
			}
			for _, p := range strings.Split(target, string(filepath.Separator)) {
				if p == ".." {
					return "", ErrDenied
				}
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(current), target)
			}
			current, err = traceLinks(target, links, visit)
			if err != nil {
				return "", err
			}
		} else if i < len(parts)-1 && !info.IsDir() {
			return "", &os.PathError{Op: "resolve", Path: current, Err: syscall.ENOTDIR}
		}
	}
	return current, nil
}

func validate(maps []Mapping) error {
	seen := map[tenant.ID]bool{}
	for _, m := range maps {
		if !m.ID.Valid() || seen[m.ID] || (m.ID == tenant.Local && m.Layout != LegacyCAS) || (m.ID != tenant.Local && m.Layout != TenantCAS) {
			return ErrConflict
		}
		seen[m.ID] = true
		for _, root := range roots(m) {
			if !filepath.IsAbs(root.abs) || !filepath.IsAbs(root.canon) {
				return ErrConflict
			}
			_, c, err := identity(root.abs)
			if err != nil {
				return err
			}
			if c != root.canon {
				return ErrConflict
			}
		}
	}
	for i, m := range maps {
		for _, other := range maps[i+1:] {
			for _, x := range roots(m) {
				for _, y := range roots(other) {
					for _, pair := range [][2]string{{x.abs, y.abs}, {x.canon, y.canon}} {
						p, q := pair[0], pair[1]
						if p == q {
							return ErrConflict
						}
						if within(p, q) && !(m.ID == tenant.Local && within(filepath.Join(p, "tenants"), q) && q != filepath.Join(p, "tenants")) {
							return ErrConflict
						}
						if within(q, p) && !(other.ID == tenant.Local && within(filepath.Join(q, "tenants"), p) && p != filepath.Join(q, "tenants")) {
							return ErrConflict
						}
					}
				}
			}
		}
	}
	return nil
}
