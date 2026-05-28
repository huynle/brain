package commands

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

const (
	attachmentCommandTimeout         = 30 * time.Second
	attachmentExtractCommandTimeout  = 2 * time.Minute
	attachmentBackfillCommandTimeout = 30 * time.Minute
)

// AttachmentFlags holds flags for brain attachments subcommands.
type AttachmentFlags struct {
	Project          string
	Entry            string
	Role             string
	Description      string
	Output           string
	Format           string
	Quiet            bool
	DryRun           bool
	Force            bool
	SkipReady        bool
	BatchSize        int
	RateLimitDelayMs int
}

// AttachmentCommand implements attachment upload, attach, list, download,
// delete, and detach subcommands.
type AttachmentCommand struct {
	Subcommand   string
	Path         string
	Entry        string
	AttachmentID string
	Config       *UnifiedConfig
	Flags        *AttachmentFlags
	Out          io.Writer

	apiClient *runner.APIClient
}

func (c *AttachmentCommand) Type() string { return "attachments" }

func (c *AttachmentCommand) Execute() error {
	out := c.Out
	if out == nil {
		out = os.Stdout
	}
	if c.Flags == nil {
		c.Flags = &AttachmentFlags{}
	}
	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()

	switch c.Subcommand {
	case "upload":
		attachment, err := c.upload(ctx, client)
		if err != nil {
			return err
		}
		return writeAttachment(out, attachment)
	case "attach":
		resp, err := c.attach(ctx, client)
		if err != nil {
			return err
		}
		return writeEntryAttachments(out, resp)
	case "list":
		if c.Entry != "" || c.Flags.Entry != "" {
			resp, err := c.listEntry(ctx, client)
			if err != nil {
				return err
			}
			return writeEntryAttachments(out, resp)
		}
		resp, err := c.listProject(ctx, client)
		if err != nil {
			return err
		}
		return writeAttachments(out, resp.Attachments)
	case "download":
		return c.download(ctx, client, out)
	case "extract":
		result, err := c.extract(ctx, client)
		if err != nil {
			return err
		}
		return writeAttachmentExtraction(out, result)
	case "backfill":
		result, err := c.backfill(ctx, client)
		if err != nil {
			return err
		}
		return writeAttachmentExtractionBackfill(out, result)
	case "detach":
		resp, err := c.detach(ctx, client)
		if err != nil {
			return err
		}
		return writeEntryAttachments(out, resp)
	case "delete":
		if err := c.delete(ctx, client); err != nil {
			return err
		}
		fmt.Fprintf(out, "Deleted: %s\n", c.AttachmentID)
		return nil
	default:
		return fmt.Errorf("usage: brain attachments <upload|attach|list|download|extract|backfill|detach|delete>")
	}
}

func (c *AttachmentCommand) timeout() time.Duration {
	switch c.Subcommand {
	case "extract":
		return attachmentExtractCommandTimeout
	case "backfill":
		return attachmentBackfillCommandTimeout
	default:
		return attachmentCommandTimeout
	}
}

func (c *AttachmentCommand) upload(ctx context.Context, client *runner.APIClient) (*types.Attachment, error) {
	if c.Path == "" {
		return nil, fmt.Errorf("usage: brain attachments upload <path> --project <project>")
	}
	project := strings.TrimSpace(c.Flags.Project)
	if project == "" {
		return nil, fmt.Errorf("--project is required")
	}
	metadata := map[string]string{}
	if c.Flags.Description != "" {
		metadata["description"] = c.Flags.Description
	}
	attachment, err := client.UploadAttachment(ctx, project, c.Path, metadata)
	if err != nil {
		return nil, fmt.Errorf("upload attachment: %w", err)
	}
	return attachment, nil
}

func (c *AttachmentCommand) attach(ctx context.Context, client *runner.APIClient) (*types.AttachEntryAttachmentResponse, error) {
	entry, attachmentID, project, err := c.entryAttachmentProject()
	if err != nil {
		return nil, err
	}
	resp, err := client.AttachEntryAttachment(ctx, project, entry, types.AttachmentReference{ID: attachmentID, Role: c.Flags.Role, Caption: c.Flags.Description})
	if err != nil {
		return nil, fmt.Errorf("attach attachment: %w", err)
	}
	return resp, nil
}

func (c *AttachmentCommand) listEntry(ctx context.Context, client *runner.APIClient) (*types.AttachEntryAttachmentResponse, error) {
	entry := c.Entry
	if entry == "" {
		entry = c.Flags.Entry
	}
	project := strings.TrimSpace(c.Flags.Project)
	if entry == "" || project == "" {
		return nil, fmt.Errorf("entry and --project are required")
	}
	resp, err := client.ListEntryAttachments(ctx, project, entry)
	if err != nil {
		return nil, fmt.Errorf("list entry attachments: %w", err)
	}
	return resp, nil
}

func (c *AttachmentCommand) listProject(ctx context.Context, client *runner.APIClient) (*types.ListAttachmentsResponse, error) {
	project := strings.TrimSpace(c.Flags.Project)
	if project == "" {
		return nil, fmt.Errorf("--project is required")
	}
	resp, err := client.ListAttachments(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	return resp, nil
}

func (c *AttachmentCommand) download(ctx context.Context, client *runner.APIClient, out io.Writer) error {
	attachmentID := strings.TrimSpace(c.AttachmentID)
	project := strings.TrimSpace(c.Flags.Project)
	if attachmentID == "" || project == "" {
		return fmt.Errorf("usage: brain attachments download <attachment-id> --project <project> [--output <path>]")
	}
	meta, data, err := client.DownloadAttachment(ctx, project, attachmentID)
	if err != nil {
		return fmt.Errorf("download attachment: %w", err)
	}
	dest := c.Flags.Output
	if dest == "" {
		dest = meta.Filename
	}
	if dest == "" || dest == "-" {
		_, err = out.Write(data)
		return err
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	fmt.Fprintf(out, "Downloaded: %s\nSHA256: %s\n", dest, runnerSHA256ForDisplay(data))
	return nil
}

func (c *AttachmentCommand) detach(ctx context.Context, client *runner.APIClient) (*types.AttachEntryAttachmentResponse, error) {
	entry, attachmentID, project, err := c.entryAttachmentProject()
	if err != nil {
		return nil, err
	}
	resp, err := client.DetachEntryAttachment(ctx, project, entry, attachmentID, c.Flags.Role)
	if err != nil {
		return nil, fmt.Errorf("detach attachment: %w", err)
	}
	return resp, nil
}

func (c *AttachmentCommand) delete(ctx context.Context, client *runner.APIClient) error {
	attachmentID := strings.TrimSpace(c.AttachmentID)
	project := strings.TrimSpace(c.Flags.Project)
	if attachmentID == "" || project == "" {
		return fmt.Errorf("usage: brain attachments delete <attachment-id> --project <project>")
	}
	return client.DeleteAttachment(ctx, project, attachmentID)
}

func (c *AttachmentCommand) extract(ctx context.Context, client *runner.APIClient) (*types.AttachmentExtractionResult, error) {
	attachmentID := strings.TrimSpace(c.AttachmentID)
	project := strings.TrimSpace(c.Flags.Project)
	if attachmentID == "" || project == "" {
		return nil, fmt.Errorf("usage: brain attachments extract <attachment-id> --project <project>")
	}
	result, err := client.ExtractAttachmentText(ctx, project, attachmentID)
	if err != nil {
		return nil, fmt.Errorf("extract attachment: %w", err)
	}
	return result, nil
}

func (c *AttachmentCommand) backfill(ctx context.Context, client *runner.APIClient) (*types.AttachmentExtractionBackfillResponse, error) {
	project := strings.TrimSpace(c.Flags.Project)
	if project == "" {
		return nil, fmt.Errorf("--project is required")
	}
	req := types.AttachmentExtractionBackfillRequest{
		DryRun:           c.Flags.DryRun,
		Force:            c.Flags.Force && !c.Flags.SkipReady,
		BatchSize:        c.Flags.BatchSize,
		RateLimitDelayMs: c.Flags.RateLimitDelayMs,
	}
	result, err := client.BackfillAttachmentExtraction(ctx, project, req)
	if err != nil {
		return nil, fmt.Errorf("backfill attachment extraction: %w", err)
	}
	return result, nil
}

func (c *AttachmentCommand) entryAttachmentProject() (string, string, string, error) {
	entry := strings.TrimSpace(c.Entry)
	if entry == "" {
		entry = strings.TrimSpace(c.Flags.Entry)
	}
	attachmentID := strings.TrimSpace(c.AttachmentID)
	project := strings.TrimSpace(c.Flags.Project)
	if entry == "" || attachmentID == "" || project == "" {
		return "", "", "", fmt.Errorf("entry, attachment ID, and --project are required")
	}
	return entry, attachmentID, project, nil
}

func (c *AttachmentCommand) getAPIClient() *runner.APIClient {
	if c.apiClient != nil {
		return c.apiClient
	}
	if c.Config == nil {
		return runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:3333", APITimeout: int((30 * time.Second) / time.Millisecond)})
	}
	return runner.NewAPIClient(c.Config.Runner)
}

func writeAttachment(out io.Writer, attachment *types.Attachment) error {
	if attachment == nil {
		return nil
	}
	_, err := fmt.Fprintf(out, "ID: %s\nFilename: %s\nContent-Type: %s\nSize: %d\nSHA256: %s\n", attachment.ID, attachment.Filename, attachment.ContentType, attachment.Size, attachment.SHA256)
	return err
}

func writeAttachments(out io.Writer, attachments []types.Attachment) error {
	for _, a := range attachments {
		if err := writeAttachment(out, &a); err != nil {
			return err
		}
	}
	return nil
}

func writeEntryAttachments(out io.Writer, resp *types.AttachEntryAttachmentResponse) error {
	if resp == nil {
		return nil
	}
	for _, a := range resp.Attachments {
		if _, err := fmt.Fprintf(out, "ID: %s\nFilename: %s\nRole: %s\nDescription: %s\n", a.ID, a.Filename, a.Role, a.Caption); err != nil {
			return err
		}
	}
	return nil
}

func writeAttachmentExtraction(out io.Writer, result *types.AttachmentExtractionResult) error {
	if result == nil {
		return nil
	}
	derived := result.DerivedText
	if _, err := fmt.Fprintf(out, "ID: %s\nStatus: %s\n", result.Attachment.ID, derived.Status); err != nil {
		return err
	}
	if provider := strings.TrimSpace(derived.Metadata["provider"]); provider != "" {
		if _, err := fmt.Fprintf(out, "Provider: %s\n", provider); err != nil {
			return err
		}
	}
	if model := strings.TrimSpace(derived.Metadata["model"]); model != "" {
		if _, err := fmt.Fprintf(out, "Model: %s\n", model); err != nil {
			return err
		}
	}
	if derived.Error != "" {
		if _, err := fmt.Fprintf(out, "Reason: %s\n", derived.Error); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "Content-Type: %s\nText-Length: %d\nLinked-Entries: %d\n", derived.ContentType, len(derived.Text), len(result.LinkedEntries)); err != nil {
		return err
	}
	for _, entry := range result.LinkedEntries {
		if entry.Role != "" {
			if _, err := fmt.Fprintf(out, "- %s (%s)\n", entry.Path, entry.Role); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(out, "- %s\n", entry.Path); err != nil {
			return err
		}
	}
	return nil
}

func writeAttachmentExtractionBackfill(out io.Writer, result *types.AttachmentExtractionBackfillResponse) error {
	if result == nil {
		return nil
	}
	if result.DryRun {
		if _, err := fmt.Fprintln(out, "DRY RUN: Attachment extraction backfill would extract matching attachments"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(out, "Attachment extraction backfill complete"); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(out, "Total: %d\nCandidates: %d\nProcessed: %d\nSkipped: %d\nFailed: %d\n", result.Total, result.Candidates, result.Processed, result.Skipped, result.Failed); err != nil {
		return err
	}

	if result.DryRun && len(result.Attachments) > 0 {
		if _, err := fmt.Fprintln(out, "Would extract:"); err != nil {
			return err
		}
		for _, item := range result.Attachments {
			if _, err := fmt.Fprintf(out, "- %s %s\n", item.AttachmentID, item.Filename); err != nil {
				return err
			}
		}
	}

	failures := attachmentBackfillFailures(result.Attachments)
	if len(failures) > 0 {
		if _, err := fmt.Fprintln(out, "Partial failures:"); err != nil {
			return err
		}
		for _, item := range failures {
			if _, err := fmt.Fprintf(out, "- %s %s: %s\n", item.AttachmentID, item.Filename, firstNonEmpty(item.Error, item.Reason)); err != nil {
				return err
			}
		}
	}

	skipped := attachmentBackfillSkipped(result.Attachments)
	if len(skipped) > 0 {
		if _, err := fmt.Fprintln(out, "Skipped attachments:"); err != nil {
			return err
		}
		for _, item := range skipped {
			if _, err := fmt.Fprintf(out, "- %s %s: %s\n", item.AttachmentID, item.Filename, firstNonEmpty(item.Reason, item.Error)); err != nil {
				return err
			}
		}
	}
	return nil
}

func attachmentBackfillFailures(items []types.AttachmentExtractionBackfillItem) []types.AttachmentExtractionBackfillItem {
	failures := make([]types.AttachmentExtractionBackfillItem, 0)
	for _, item := range items {
		if item.Error != "" || strings.EqualFold(item.Status, string(types.AttachmentExtractionStatusFailed)) {
			failures = append(failures, item)
		}
	}
	return failures
}

func attachmentBackfillSkipped(items []types.AttachmentExtractionBackfillItem) []types.AttachmentExtractionBackfillItem {
	skipped := make([]types.AttachmentExtractionBackfillItem, 0)
	for _, item := range items {
		if item.Skipped {
			skipped = append(skipped, item)
		}
	}
	return skipped
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "n/a"
}

func runnerSHA256ForDisplay(data []byte) string {
	// DownloadAttachment already verifies server-provided SHA256. This display
	// value keeps the CLI output useful even when metadata has no checksum.
	return fmt.Sprintf("%x", dataHash(data))
}

func dataHash(data []byte) [32]byte {
	return sha256.Sum256(data)
}
