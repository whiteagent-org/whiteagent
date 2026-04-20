// Package scheduler provides a poll-based scheduler that fires due cron entries
// through the transport bus to the agent loop.
package scheduler

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/cron"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

// cronStore defines the store methods the scheduler needs.
type cronStore interface {
	ListActiveCronEntries(ctx context.Context) ([]entity.CronEntry, error)
	UpdateCronStatus(ctx context.Context, tenantID entity.TenantID, id entity.CronEntryID, status string) error
	UpdateCronNextRun(ctx context.Context, tenantID entity.TenantID, id entity.CronEntryID, nextRunAt *time.Time) error
	InsertCronRun(ctx context.Context, tenantID entity.TenantID, run entity.CronRun) error
	UpdateCronRun(ctx context.Context, tenantID entity.TenantID, runID entity.CronRunID, status string, errMsg string, finishedAt *time.Time) error
	GetMessages(ctx context.Context, tenantID entity.TenantID, filter port.MessageFilter) ([]entity.Message, error)
}

// cronTransport defines the transport method the scheduler needs.
type cronTransport interface {
	Publish(ctx context.Context, topic string, msg entity.Message) error
}

// Scheduler polls the store at a fixed interval and fires due cron entries
// by publishing synthetic messages to TopicInbound.
type Scheduler struct {
	store     cronStore
	transport cronTransport
	scopedFS  port.ScopedFS
	interval  time.Duration
	timezone  string
	cancel    context.CancelFunc
	done      chan struct{}
}

// New creates a scheduler that polls store every interval.
// The timezone parameter is an IANA timezone string (e.g. "America/New_York")
// used for computing NextAfter on recurring entries. Empty defaults to "UTC".
func New(store cronStore, transport cronTransport, scopedFS port.ScopedFS, interval time.Duration, timezone string) *Scheduler {
	if timezone == "" {
		timezone = "UTC"
	}
	return &Scheduler{
		store:     store,
		transport: transport,
		scopedFS:  scopedFS,
		interval:  interval,
		timezone:  timezone,
	}
}

// Start spawns the poll loop goroutine.
func (s *Scheduler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	go s.run(ctx)
}

// Stop cancels the poll loop and waits for it to exit.
func (s *Scheduler) Stop() {
	if s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
}

// run is the poll loop that fires on each ticker interval.
func (s *Scheduler) run(ctx context.Context) {
	defer close(s.done)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			s.tick(ctx, t)
		}
	}
}

// tick queries the store for active cron entries and fires any that are due.
func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	now = now.UTC()
	entries, err := s.store.ListActiveCronEntries(ctx)
	if err != nil {
		slog.Error("scheduler.tick", "err", err)
		return
	}

	fired := 0
	for _, entry := range entries {
		if entry.NextRunAt == nil || entry.NextRunAt.After(now) {
			continue
		}
		s.fireEntry(ctx, entry, now)
		fired++
	}

	if fired > 0 {
		slog.Info("scheduler.tick", "fired", fired, "total", len(entries))
	}
}

// fireEntry processes a single due cron entry: updates state, creates a run
// record, resets conversation, and publishes the synthetic message.
func (s *Scheduler) fireEntry(ctx context.Context, entry entity.CronEntry, now time.Time) {
	slog.Info("scheduler.fire", "entry_id", entry.ID, "tenant_id", entry.TenantID, "type", entry.Type)

	// 1. Update entry state BEFORE publish.
	switch entry.Type {
	case "once":
		if err := s.store.UpdateCronStatus(ctx, entry.TenantID, entry.ID, "completed"); err != nil {
			slog.Error("scheduler.fire.update_status", "entry_id", entry.ID, "err", err)
			return
		}
		if err := s.store.UpdateCronNextRun(ctx, entry.TenantID, entry.ID, nil); err != nil {
			slog.Error("scheduler.fire.update_next_run", "entry_id", entry.ID, "err", err)
			return
		}
	case "recurring":
		parsed, err := cron.Parse(entry.CronExpr)
		if err != nil {
			slog.Error("scheduler.fire.parse_cron", "entry_id", entry.ID, "expr", entry.CronExpr, "err", err)
			return
		}
		loc, locErr := time.LoadLocation(s.timezone)
		if locErr != nil {
			loc = time.UTC
		}
		next := parsed.NextAfter(now.In(loc))
		next = next.UTC()
		if err := s.store.UpdateCronNextRun(ctx, entry.TenantID, entry.ID, &next); err != nil {
			slog.Error("scheduler.fire.update_next_run", "entry_id", entry.ID, "err", err)
			return
		}
	}

	// 2. Insert CronRun record.
	runID := entity.CronRunID(util.NewID())
	run := entity.CronRun{
		ID:          runID,
		CronEntryID: entry.ID,
		TenantID:    entry.TenantID,
		Status:      "running",
		StartedAt:   now,
	}
	if err := s.store.InsertCronRun(ctx, entry.TenantID, run); err != nil {
		slog.Error("scheduler.fire.insert_run", "entry_id", entry.ID, "err", err)
		return
	}

	// 3. Build synthetic message with ChatID only. Delivery is resolved from
	// the chat entity at runtime by the outbound handler / mapper.
	msg := entity.Message{
		ID:             entity.MessageID(util.NewID()),
		TenantID:       entry.TenantID,
		UserID:         entry.UserID,
		AgentID:        entry.AgentID,
		ChatID:         entry.ChatID,
		IsGroup:        entry.IsGroup,
		Kind:           entity.MessageKindCron,
		Role:           entity.RoleUser,
		Content:        entry.Instructions,
		Metadata:       entry.MessageMetadata(),
		ConversationID: entry.ConversationID,
		CreatedAt:      now,
	}

	// 3b. Copy attachment files from the original message into a new directory
	// owned by this synthetic message, so each cron firing has independent copies.
	if !entry.MessageID.IsEmpty() && s.scopedFS != nil {
		origMsgs, err := s.store.GetMessages(ctx, entry.TenantID, port.MessageFilter{MessageID: entry.MessageID})
		if err != nil {
			slog.Warn("scheduler.fire.get_attachments", "entry_id", entry.ID, "msg_id", entry.MessageID, "err", err)
		} else if len(origMsgs) > 0 {
			copied := copyAttachmentFiles(s.scopedFS, string(msg.ID), origMsgs[0].Attachments)
			if len(copied) > 0 {
				msg.Attachments = copied
			}
		}
	}

	// 4. Publish to inbound topic.
	if err := s.transport.Publish(ctx, entity.TopicInbound, msg); err != nil {
		slog.Error("scheduler.fire.publish", "entry_id", entry.ID, "err", err)
		finishedAt := now
		if updateErr := s.store.UpdateCronRun(ctx, entry.TenantID, runID, "failed", err.Error(), &finishedAt); updateErr != nil {
			slog.Error("scheduler.fire.update_run", "run_id", runID, "err", updateErr)
		}
		return
	}

	// 5. Mark run as success.
	finishedAt := now
	if updateErr := s.store.UpdateCronRun(ctx, entry.TenantID, runID, "success", "", &finishedAt); updateErr != nil {
		slog.Error("scheduler.fire.update_run_success", "run_id", runID, "err", updateErr)
	}
}

// copyAttachmentFiles physically copies attachment files into a new ScopedFS
// directory so each cron firing owns independent file copies. Individual file
// failures are logged and skipped (partial success is acceptable).
func copyAttachmentFiles(fs port.ScopedFS, dirID string, atts []entity.Attachment) []entity.Attachment {
	if len(atts) == 0 {
		return nil
	}

	dir, err := fs.EnsureDir(port.ScopeCron, dirID)
	if err != nil {
		slog.Warn("scheduler.copy_attachments.ensure_dir", "dir_id", dirID, "err", err)
		return nil
	}

	var copied []entity.Attachment
	for _, att := range atts {
		dst := filepath.Join(dir, att.Filename)
		if err := copyFile(att.Path, dst); err != nil {
			slog.Warn("scheduler.copy_attachments.copy_file", "src", att.Path, "dst", dst, "err", err)
			continue
		}
		copied = append(copied, entity.Attachment{
			ID:       att.ID,
			Kind:     att.Kind,
			Filename: att.Filename,
			MimeType: att.MimeType,
			Size:     att.Size,
			Path:     dst,
			Caption:  att.Caption,
		})
	}
	return copied
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
