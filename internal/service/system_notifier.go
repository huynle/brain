package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// SystemNotifier raises system-generated attention notices (scheduling
// reports, calendar warnings, and similar).
type SystemNotifier interface {
	Notify(ctx context.Context, n SystemNotice) error
}

// SystemNotice is one system-generated notice. DedupKey makes a repeated
// notice idempotent per recipient, so an identical notice is not re-raised.
type SystemNotice struct {
	Kind       string
	Severity   string
	Title      string
	Body       string
	Project    string
	SourceType string
	DedupKey   string
}

// systemNotifier delivers system notices as attention items through the
// attention service. It is the adapter the rest of the server depends on, so
// callers never touch attention storage directly.
type systemNotifier struct {
	attention  *AttentionService
	configured []string
}

// NewSystemNotifier builds the adapter. configured holds the token names from
// server.attention.system_recipients; blank and repeated names are dropped.
// When configured is empty, notices fan out to the recipients that already own
// an attention item (see resolveSystemRecipients).
func NewSystemNotifier(attention *AttentionService, configured []string) SystemNotifier {
	return &systemNotifier{attention: attention, configured: cleanRecipients(configured)}
}

// Notify creates one attention item per resolved recipient. Creation is
// idempotent per (recipient, DedupKey). A notice with no recipient is logged
// and returns nil: a missing audience must never fail the caller. A nil
// notifier or attention service is a no-op.
func (n *systemNotifier) Notify(ctx context.Context, notice SystemNotice) error {
	if n == nil || n.attention == nil {
		return nil
	}
	if notice.Kind == "" || notice.Title == "" {
		return fmt.Errorf("system notice requires kind and title")
	}
	recipients, err := n.resolveSystemRecipients(ctx)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		slog.Warn("system notice has no attention recipients; logged only",
			"kind", notice.Kind, "title", notice.Title, "dedup_key", notice.DedupKey)
		return nil
	}
	var errs []error
	for _, recipient := range recipients {
		_, err := n.attention.CreateAttention(ctx, "", types.CreateAttentionRequest{
			Recipient:  recipient,
			Kind:       notice.Kind,
			Severity:   notice.Severity,
			Title:      notice.Title,
			Body:       notice.Body,
			Project:    notice.Project,
			SourceType: notice.SourceType,
			DedupKey:   notice.DedupKey,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("notify %s: %w", recipient, err))
		}
	}
	return errors.Join(errs...)
}

// resolveSystemRecipients returns who receives system notices. Configured
// token names always win. Otherwise the audience is every recipient that
// already owns an attention item. Recipients known only by a registered push
// device are not included: phonepush exposes no owner read, and this change is
// limited to the attention store.
func (n *systemNotifier) resolveSystemRecipients(ctx context.Context) ([]string, error) {
	if len(n.configured) > 0 {
		return n.configured, nil
	}
	return n.attention.ListAttentionRecipients(ctx)
}

// NotifySystem delivers a notice through n. A nil notifier is a no-op, so a
// consumer wired before the notifier exists does nothing rather than failing.
func NotifySystem(ctx context.Context, n SystemNotifier, notice SystemNotice) error {
	if n == nil {
		return nil
	}
	return n.Notify(ctx, notice)
}

// cleanRecipients trims names, drops blanks, and keeps the first of each name.
func cleanRecipients(names []string) []string {
	var out []string
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
