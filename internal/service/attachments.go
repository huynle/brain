package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// Compile-time check that AttachmentServiceImpl implements api.AttachmentService.
var _ api.AttachmentService = (*AttachmentServiceImpl)(nil)

var errAttachmentServiceNotImplemented = errors.New("attachment service behavior not implemented")

const defaultAttachmentMaxSizeBytes int64 = 25 << 20 // 25 MiB

// AttachmentServiceImpl orchestrates attachment blob storage, metadata storage,
// and entry association.
type AttachmentServiceImpl struct {
	storage      *storage.StorageLayer
	blobs        blobstore.Store
	brain        api.BrainService
	maxSizeBytes int64
}

// NewAttachmentService creates an attachment orchestration service.
func NewAttachmentService(store *storage.StorageLayer, blobs blobstore.Store, brain api.BrainService, maxSizeBytes int64) *AttachmentServiceImpl {
	if maxSizeBytes <= 0 {
		maxSizeBytes = defaultAttachmentMaxSizeBytes
	}
	return &AttachmentServiceImpl{
		storage:      store,
		blobs:        blobs,
		brain:        brain,
		maxSizeBytes: maxSizeBytes,
	}
}

// Create stores binary content and creates/reuses attachment metadata for a project.
func (s *AttachmentServiceImpl) Create(ctx context.Context, projectID string, req types.CreateAttachmentRequest, content io.Reader) (*types.CreateAttachmentResponse, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if err := validateAttachmentProject(projectID); err != nil {
		return nil, err
	}
	if err := validateAttachmentFilename(req.Filename); err != nil {
		return nil, err
	}
	if err := validateAttachmentSize(req.Size, s.maxSizeBytes); err != nil {
		return nil, err
	}
	if content == nil {
		return nil, errors.New("attachment content is required")
	}

	data, err := readAttachmentContent(content, s.maxSizeBytes)
	if err != nil {
		return nil, err
	}
	if err := validateAttachmentSize(int64(len(data)), s.maxSizeBytes); err != nil {
		return nil, err
	}
	digest := attachmentDigest(data)
	if err := validateAttachmentSHA256(req.SHA256, digest); err != nil {
		return nil, err
	}
	blobExisted := s.blobExists(digest)
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	mediaType := sniffAttachmentContentType(req.ContentType, sample)
	storedDigest, size, err := s.blobs.Put(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if storedDigest != digest {
		if !blobExisted {
			_ = s.blobs.Delete(storedDigest)
		}
		return nil, errors.New("attachment blob digest mismatch")
	}
	if err := validateAttachmentSize(size, s.maxSizeBytes); err != nil {
		if !blobExisted {
			_ = s.blobs.Delete(digest)
		}
		return nil, err
	}

	metadata, err := attachmentMetadataToJSON(req, projectID)
	if err != nil {
		if !blobExisted {
			_ = s.blobs.Delete(digest)
		}
		return nil, err
	}
	row, err := s.storage.CreateAttachment(ctx, storage.AttachmentInput{
		Digest:    digest,
		Size:      size,
		MediaType: mediaType,
		Metadata:  metadata,
	})
	if err != nil {
		if !blobExisted {
			_ = s.blobs.Delete(digest)
		}
		return nil, err
	}
	if !attachmentRowBelongsToProject(row, projectID) {
		return nil, api.ErrNotFound
	}
	att, err := attachmentRowToDTO(row)
	if err != nil {
		return nil, err
	}
	return &types.CreateAttachmentResponse{Attachment: att}, nil
}

// Get returns attachment metadata by ID within a project.
func (s *AttachmentServiceImpl) Get(ctx context.Context, projectID, attachmentID string) (*types.Attachment, error) {
	row, err := s.getProjectAttachmentRow(ctx, projectID, attachmentID)
	if err != nil {
		return nil, err
	}
	att, err := attachmentRowToDTO(row)
	if err != nil {
		return nil, err
	}
	return &att, nil
}

// Open returns attachment metadata plus a readable content stream.
func (s *AttachmentServiceImpl) Open(ctx context.Context, projectID, attachmentID string) (*types.Attachment, io.ReadCloser, error) {
	att, err := s.Get(ctx, projectID, attachmentID)
	if err != nil {
		return nil, nil, err
	}
	stream, err := s.blobs.Get(att.StorageKey)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) || errors.Is(err, blobstore.ErrInvalidHash) {
			return nil, nil, api.ErrNotFound
		}
		return nil, nil, err
	}
	return att, stream, nil
}

// OpenText returns attachment text when text is derivable from the original blob.
// Minimal derived text behavior treats textual media types as their own text and
// reports not found for non-text blobs with no derived text available.
func (s *AttachmentServiceImpl) OpenText(ctx context.Context, projectID, attachmentID string) (*types.Attachment, io.ReadCloser, error) {
	att, stream, err := s.Open(ctx, projectID, attachmentID)
	if err != nil {
		return nil, nil, err
	}
	if !isTextualAttachmentContentType(att.ContentType) {
		_ = stream.Close()
		return nil, nil, api.ErrNotFound
	}
	return att, stream, nil
}

// List returns attachments visible within a project.
func (s *AttachmentServiceImpl) List(ctx context.Context, projectID string) (*types.ListAttachmentsResponse, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if err := validateAttachmentProject(projectID); err != nil {
		return nil, err
	}
	rows, err := s.storage.ListAttachments(ctx)
	if err != nil {
		return nil, err
	}
	attachments := make([]types.Attachment, 0, len(rows))
	for _, row := range rows {
		if !attachmentRowBelongsToProject(row, projectID) {
			continue
		}
		att, err := attachmentRowToDTO(row)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, att)
	}
	return &types.ListAttachmentsResponse{Attachments: attachments, Total: len(attachments)}, nil
}

// ListForEntry returns attachment references associated with a brain entry.
func (s *AttachmentServiceImpl) ListForEntry(ctx context.Context, projectID, pathOrID string) (*types.AttachEntryAttachmentResponse, error) {
	if err := s.ensureReadyForEntryAssociation(); err != nil {
		return nil, err
	}
	entry, err := s.getProjectEntry(ctx, projectID, pathOrID)
	if err != nil {
		return nil, err
	}
	if _, err := s.storage.ListAttachmentsForEntry(ctx, entry.Path); err != nil {
		return nil, normalizeAttachmentStorageNotFound(err)
	}
	return attachmentEntryResponse(entry), nil
}

// Attach links an existing attachment to a brain entry.
func (s *AttachmentServiceImpl) Attach(ctx context.Context, projectID, pathOrID string, req types.AttachEntryAttachmentRequest) (*types.AttachEntryAttachmentResponse, error) {
	if err := s.ensureReadyForEntryAssociation(); err != nil {
		return nil, err
	}
	row, err := s.getProjectAttachmentRow(ctx, projectID, req.Attachment.ID)
	if err != nil {
		return nil, err
	}
	role := strings.TrimSpace(req.Attachment.Role)
	if err := validateAttachmentRole(role); err != nil {
		return nil, err
	}
	entry, err := s.getProjectEntry(ctx, projectID, pathOrID)
	if err != nil {
		return nil, err
	}

	linkExisted, err := s.entryAttachmentLinkExists(ctx, entry.Path, row.ID, role)
	if err != nil {
		return nil, err
	}
	if err := s.storage.LinkAttachmentToEntry(ctx, entry.Path, row.ID, role); err != nil {
		return nil, normalizeAttachmentStorageNotFound(err)
	}

	att, err := attachmentRowToDTO(row)
	if err != nil {
		if !linkExisted {
			_, _ = s.storage.UnlinkAttachmentFromEntry(ctx, entry.Path, row.ID, role)
		}
		return nil, err
	}
	updatedRefs := upsertAttachmentReference(entry.Attachments, attachmentDTOToReference(att, req.Attachment, role))
	updatedEntry, err := s.updateEntryAttachments(ctx, pathOrID, updatedRefs)
	if err != nil {
		if !linkExisted {
			_, _ = s.storage.UnlinkAttachmentFromEntry(ctx, entry.Path, row.ID, role)
		}
		return nil, err
	}
	return attachmentEntryResponse(updatedEntry), nil
}

// Detach removes an attachment link from a brain entry.
func (s *AttachmentServiceImpl) Detach(ctx context.Context, projectID, pathOrID, attachmentID, role string) (*types.AttachEntryAttachmentResponse, error) {
	if err := s.ensureReadyForEntryAssociation(); err != nil {
		return nil, err
	}
	row, err := s.getProjectAttachmentRow(ctx, projectID, attachmentID)
	if err != nil {
		return nil, err
	}
	role = strings.TrimSpace(role)
	if err := validateAttachmentRole(role); err != nil {
		return nil, err
	}
	entry, err := s.getProjectEntry(ctx, projectID, pathOrID)
	if err != nil {
		return nil, err
	}

	unlinked, err := s.storage.UnlinkAttachmentFromEntry(ctx, entry.Path, row.ID, role)
	if err != nil {
		return nil, normalizeAttachmentStorageNotFound(err)
	}
	updatedRefs := removeAttachmentReference(entry.Attachments, strconv.FormatInt(row.ID, 10), role)
	updatedEntry, err := s.updateEntryAttachments(ctx, pathOrID, updatedRefs)
	if err != nil {
		if unlinked {
			_ = s.storage.LinkAttachmentToEntry(ctx, entry.Path, row.ID, role)
		}
		return nil, err
	}
	return attachmentEntryResponse(updatedEntry), nil
}

// Delete removes an attachment only when it is safe to do so.
func (s *AttachmentServiceImpl) Delete(ctx context.Context, projectID, attachmentID string) (bool, error) {
	row, err := s.getProjectAttachmentRow(ctx, projectID, attachmentID)
	if err != nil {
		return false, err
	}
	deleted, err := s.storage.DeleteAttachmentIfUnreferenced(ctx, row.ID)
	if err != nil {
		return false, err
	}
	if !deleted {
		return false, nil
	}
	if err := s.blobs.Delete(row.Digest); err != nil && !errors.Is(err, blobstore.ErrNotFound) {
		return false, err
	}
	return true, nil
}

func (s *AttachmentServiceImpl) ensureReady() error {
	if s == nil || s.storage == nil || s.blobs == nil {
		return errors.New("attachment service dependencies are required")
	}
	return nil
}

func (s *AttachmentServiceImpl) ensureReadyForEntryAssociation() error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	if s.brain == nil {
		return errors.New("attachment service brain dependency is required")
	}
	return nil
}

func (s *AttachmentServiceImpl) getProjectEntry(ctx context.Context, projectID, pathOrID string) (*types.BrainEntry, error) {
	if err := validateAttachmentProject(projectID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(pathOrID) == "" {
		return nil, errors.New("entry path or id is required")
	}
	entry, err := s.brain.Recall(ctx, pathOrID)
	if err != nil {
		return nil, err
	}
	if entry == nil || !entryBelongsToAttachmentProject(entry, projectID) {
		return nil, api.ErrNotFound
	}
	return entry, nil
}

func entryBelongsToAttachmentProject(entry *types.BrainEntry, projectID string) bool {
	projectID = strings.TrimSpace(projectID)
	if entry == nil {
		return false
	}
	if strings.TrimSpace(entry.ProjectID) != "" {
		return strings.TrimSpace(entry.ProjectID) == projectID
	}
	return strings.HasPrefix(entry.Path, "projects/"+projectID+"/")
}

func (s *AttachmentServiceImpl) entryAttachmentLinkExists(ctx context.Context, notePath string, attachmentID int64, role string) (bool, error) {
	refs, err := s.storage.ListEntryReferencesForAttachment(ctx, attachmentID)
	if err != nil {
		return false, err
	}
	for _, ref := range refs {
		if ref.NotePath == notePath && ref.Role == role {
			return true, nil
		}
	}
	return false, nil
}

func (s *AttachmentServiceImpl) updateEntryAttachments(ctx context.Context, pathOrID string, refs []types.AttachmentReference) (*types.BrainEntry, error) {
	refsCopy := append([]types.AttachmentReference(nil), refs...)
	updated, err := s.brain.Update(ctx, pathOrID, types.UpdateEntryRequest{Attachments: &refsCopy})
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, api.ErrNotFound
	}
	return updated, nil
}

func (s *AttachmentServiceImpl) getProjectAttachmentRow(ctx context.Context, projectID, attachmentID string) (*storage.AttachmentRow, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if err := validateAttachmentProject(projectID); err != nil {
		return nil, err
	}
	id, err := parseAttachmentID(attachmentID)
	if err != nil {
		return nil, err
	}
	row, err := s.storage.GetAttachment(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil || !attachmentRowBelongsToProject(row, projectID) {
		return nil, api.ErrNotFound
	}
	return row, nil
}

func readAttachmentContent(content io.Reader, maxSizeBytes int64) ([]byte, error) {
	if maxSizeBytes <= 0 {
		return io.ReadAll(content)
	}
	data, err := io.ReadAll(io.LimitReader(content, maxSizeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read attachment content: %w", err)
	}
	if int64(len(data)) > maxSizeBytes {
		return nil, blobstore.ErrTooLarge
	}
	return data, nil
}

func attachmentDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateAttachmentSHA256(declared, actual string) error {
	declared = strings.TrimSpace(strings.ToLower(declared))
	if declared == "" {
		return nil
	}
	declared = strings.TrimPrefix(declared, "sha256:")
	if declared != actual {
		return errors.New("attachment sha256 does not match content")
	}
	return nil
}

func (s *AttachmentServiceImpl) blobExists(digest string) bool {
	stream, err := s.blobs.Get(digest)
	if err != nil {
		return false
	}
	_ = stream.Close()
	return true
}

func attachmentRowBelongsToProject(row *storage.AttachmentRow, projectID string) bool {
	metadata, err := attachmentMetadataFromJSON(row.Metadata)
	if err != nil {
		return false
	}
	return metadata["project_id"] == strings.TrimSpace(projectID)
}

func validateAttachmentProject(projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New("attachment project id is required")
	}
	return nil
}

func validateAttachmentFilename(filename string) error {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return errors.New("attachment filename is required")
	}
	if filename != filepath.Base(filename) || strings.ContainsAny(filename, "\\/") {
		return fmt.Errorf("attachment filename %q is unsafe", filename)
	}
	return nil
}

func parseAttachmentID(id string) (int64, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0, errors.New("attachment id is required")
	}
	parsed, err := strconv.ParseInt(id, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("attachment id %q is unsafe", id)
	}
	return parsed, nil
}

func validateAttachmentRole(role string) error {
	if role == "" {
		return errors.New("attachment role is required")
	}
	for _, r := range role {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return fmt.Errorf("attachment role %q is unsafe", role)
	}
	return nil
}

func normalizeAttachmentStorageNotFound(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "note not found") || strings.Contains(err.Error(), "attachment not found") {
		return api.ErrNotFound
	}
	return err
}

func attachmentDTOToReference(att types.Attachment, requested types.AttachmentReference, role string) types.AttachmentReference {
	return types.AttachmentReference{
		ID:          att.ID,
		Filename:    att.Filename,
		ContentType: att.ContentType,
		Size:        att.Size,
		SHA256:      att.SHA256,
		Role:        role,
		Caption:     requested.Caption,
		Derived:     append([]types.AttachmentDerived(nil), requested.Derived...),
	}
}

func upsertAttachmentReference(refs []types.AttachmentReference, next types.AttachmentReference) []types.AttachmentReference {
	updated := make([]types.AttachmentReference, 0, len(refs)+1)
	replaced := false
	for _, ref := range refs {
		if ref.ID == next.ID && strings.TrimSpace(ref.Role) == next.Role {
			if !replaced {
				updated = append(updated, next)
				replaced = true
			}
			continue
		}
		updated = append(updated, ref)
	}
	if !replaced {
		updated = append(updated, next)
	}
	return updated
}

func removeAttachmentReference(refs []types.AttachmentReference, attachmentID, role string) []types.AttachmentReference {
	updated := make([]types.AttachmentReference, 0, len(refs))
	for _, ref := range refs {
		if ref.ID == attachmentID && strings.TrimSpace(ref.Role) == role {
			continue
		}
		updated = append(updated, ref)
	}
	return updated
}

func attachmentEntryResponse(entry *types.BrainEntry) *types.AttachEntryAttachmentResponse {
	return &types.AttachEntryAttachmentResponse{
		EntryID:     entry.ID,
		Path:        entry.Path,
		Attachments: append([]types.AttachmentReference(nil), entry.Attachments...),
	}
}

func validateAttachmentSize(size, maxSizeBytes int64) error {
	if size < 0 {
		return errors.New("attachment size must be non-negative")
	}
	if maxSizeBytes > 0 && size > maxSizeBytes {
		return blobstore.ErrTooLarge
	}
	return nil
}

func sniffAttachmentContentType(declared string, sample []byte) string {
	declared = strings.TrimSpace(declared)
	if declared != "" && declared != "application/octet-stream" {
		return declared
	}
	return http.DetectContentType(sample)
}

func isTextualAttachmentContentType(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	switch mediaType {
	case "application/json", "application/xml", "application/x-yaml", "application/yaml", "application/javascript":
		return true
	default:
		return false
	}
}

func attachmentRowToDTO(row *storage.AttachmentRow) (types.Attachment, error) {
	if row == nil {
		return types.Attachment{}, errors.New("attachment row is nil")
	}
	metadata, err := attachmentMetadataFromJSON(row.Metadata)
	if err != nil {
		return types.Attachment{}, err
	}
	filename := metadata["filename"]
	delete(metadata, "filename")

	return types.Attachment{
		ID:          strconv.FormatInt(row.ID, 10),
		Filename:    filename,
		ContentType: row.MediaType,
		Size:        row.Size,
		SHA256:      row.Digest,
		StorageKey:  row.Digest,
		Created:     row.CreatedAt,
		Modified:    row.CreatedAt,
		Metadata:    metadata,
	}, nil
}

func attachmentMetadataFromJSON(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}, nil
	}
	metadata := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil, fmt.Errorf("parse attachment metadata: %w", err)
	}
	return metadata, nil
}

func attachmentMetadataToJSON(req types.CreateAttachmentRequest, projectID string) (string, error) {
	metadata := make(map[string]string, len(req.Metadata)+2)
	for k, v := range req.Metadata {
		metadata[k] = v
	}
	metadata["filename"] = strings.TrimSpace(req.Filename)
	metadata["project_id"] = strings.TrimSpace(projectID)

	data, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("encode attachment metadata: %w", err)
	}
	return string(data), nil
}
