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

// AttachmentFlags holds flags for brain attachments subcommands.
type AttachmentFlags struct {
	Project     string
	Entry       string
	Role        string
	Description string
	Output      string
	Format      string
	Quiet       bool
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		return fmt.Errorf("usage: brain attachments <upload|attach|list|download|detach|delete>")
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

func runnerSHA256ForDisplay(data []byte) string {
	// DownloadAttachment already verifies server-provided SHA256. This display
	// value keeps the CLI output useful even when metadata has no checksum.
	return fmt.Sprintf("%x", dataHash(data))
}

func dataHash(data []byte) [32]byte {
	return sha256.Sum256(data)
}
