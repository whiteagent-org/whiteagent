package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mock store (only cron methods needed)
// ---------------------------------------------------------------------------

type mockStore struct {
	entries        []entity.CronEntry
	listErr        error
	statusCalls    []statusCall
	nextRunCalls   []nextRunCall
	insertedRuns   []entity.CronRun
	updatedRuns    []updatedRun
	updateRunErr   error
	statusErr      error
	nextRunErr     error
	insertRunErr   error
	messages       map[entity.MessageID]entity.Message
	getMessagesErr error
}

type statusCall struct {
	tenantID entity.TenantID
	id       entity.CronEntryID
	status   string
}

type nextRunCall struct {
	tenantID  entity.TenantID
	id        entity.CronEntryID
	nextRunAt *time.Time
}

type updatedRun struct {
	tenantID   entity.TenantID
	runID      entity.CronRunID
	status     string
	errMsg     string
	finishedAt *time.Time
}

func (m *mockStore) ListActiveCronEntries(_ context.Context) ([]entity.CronEntry, error) {
	return m.entries, m.listErr
}

func (m *mockStore) UpdateCronStatus(_ context.Context, tenantID entity.TenantID, id entity.CronEntryID, status string) error {
	m.statusCalls = append(m.statusCalls, statusCall{tenantID, id, status})
	return m.statusErr
}

func (m *mockStore) UpdateCronNextRun(_ context.Context, tenantID entity.TenantID, id entity.CronEntryID, nextRunAt *time.Time) error {
	m.nextRunCalls = append(m.nextRunCalls, nextRunCall{tenantID, id, nextRunAt})
	return m.nextRunErr
}

func (m *mockStore) InsertCronRun(_ context.Context, _ entity.TenantID, run entity.CronRun) error {
	m.insertedRuns = append(m.insertedRuns, run)
	return m.insertRunErr
}

func (m *mockStore) UpdateCronRun(_ context.Context, tenantID entity.TenantID, runID entity.CronRunID, status string, errMsg string, finishedAt *time.Time) error {
	m.updatedRuns = append(m.updatedRuns, updatedRun{tenantID, runID, status, errMsg, finishedAt})
	return m.updateRunErr
}

func (m *mockStore) GetMessages(_ context.Context, _ entity.TenantID, filter port.MessageFilter) ([]entity.Message, error) {
	if m.getMessagesErr != nil {
		return nil, m.getMessagesErr
	}
	if msg, ok := m.messages[filter.MessageID]; ok {
		return []entity.Message{msg}, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Mock transport
// ---------------------------------------------------------------------------

type mockTransport struct {
	mu       sync.Mutex
	messages []entity.Message
	err      error
}

func (m *mockTransport) Publish(_ context.Context, topic string, msg entity.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.messages = append(m.messages, msg)
	return nil
}

// ---------------------------------------------------------------------------
// Mock ScopedFS
// ---------------------------------------------------------------------------

type mockScopedFS struct {
	root      string
	ensureErr error
}

func newMockScopedFS(t *testing.T) *mockScopedFS {
	t.Helper()
	return &mockScopedFS{root: t.TempDir()}
}

func (m *mockScopedFS) EnsureDir(_ port.Scope, id string) (string, error) {
	if m.ensureErr != nil {
		return "", m.ensureErr
	}
	dir := filepath.Join(m.root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (m *mockScopedFS) GetDir(_ port.Scope, id string) (string, error) {
	dir := filepath.Join(m.root, id)
	if _, err := os.Stat(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func (m *mockScopedFS) BaseDir() string { return m.root }

func (m *mockScopedFS) Cleanup(_ port.Scope, id string) error {
	return os.RemoveAll(filepath.Join(m.root, id))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func fixedNow() time.Time {
	return time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
}

func dueEntry(typ string) entity.CronEntry {
	due := fixedNow().Add(-time.Minute)
	return entity.CronEntry{
		ID:             "cron-1",
		TenantID:       "t1",
		AgentID:        "a1",
		UserID:         "u1",
		ChatID:         "chat-1",
		IsGroup:        false,
		Name:           "daily report",
		Instructions:   "Generate daily report",
		Type:           typ,
		CronExpr:       "0 10 * * *",
		NextRunAt:      &due,
		Status:         "active",
		CreatedAt:      fixedNow().Add(-24 * time.Hour),
		Metadata:       map[string]string{"sender_name": "Test User"},
		ConversationID: "conv-orig-1",
	}
}

func TestTick_FiresDueRecurringEntry(t *testing.T) {
	store := &mockStore{entries: []entity.CronEntry{dueEntry("recurring")}}
	transport := &mockTransport{}
	s := New(store, transport, nil, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	// NextRunAt should be updated (advanced)
	if len(store.nextRunCalls) != 1 {
		t.Fatalf("expected 1 nextRun call, got %d", len(store.nextRunCalls))
	}
	nrc := store.nextRunCalls[0]
	if nrc.tenantID != "t1" || nrc.id != "cron-1" {
		t.Errorf("wrong nextRun call target: %+v", nrc)
	}
	if nrc.nextRunAt == nil {
		t.Fatal("recurring entry nextRunAt should not be nil")
	}
	// Next run should be after now
	if !nrc.nextRunAt.After(fixedNow()) {
		t.Errorf("next run %v should be after now %v", nrc.nextRunAt, fixedNow())
	}

	// CronRun inserted
	if len(store.insertedRuns) != 1 {
		t.Fatalf("expected 1 inserted run, got %d", len(store.insertedRuns))
	}
	run := store.insertedRuns[0]
	if run.Status != "running" {
		t.Errorf("expected status running, got %s", run.Status)
	}
	if run.CronEntryID != "cron-1" {
		t.Errorf("expected cron entry id cron-1, got %s", run.CronEntryID)
	}

	// Transport publish called
	if len(transport.messages) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(transport.messages))
	}

	// After successful publish, UpdateCronRun should be called with "success"
	if len(store.updatedRuns) != 1 {
		t.Fatalf("expected 1 updated run (success), got %d", len(store.updatedRuns))
	}
	if store.updatedRuns[0].status != "success" {
		t.Errorf("expected updated run status success, got %s", store.updatedRuns[0].status)
	}
}

func TestTick_FiresDueOneShotEntry(t *testing.T) {
	store := &mockStore{entries: []entity.CronEntry{dueEntry("once")}}
	transport := &mockTransport{}
	s := New(store, transport, nil, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	// Status set to completed
	if len(store.statusCalls) != 1 {
		t.Fatalf("expected 1 status call, got %d", len(store.statusCalls))
	}
	if store.statusCalls[0].status != "completed" {
		t.Errorf("expected status completed, got %s", store.statusCalls[0].status)
	}

	// NextRunAt set to nil
	if len(store.nextRunCalls) != 1 {
		t.Fatalf("expected 1 nextRun call, got %d", len(store.nextRunCalls))
	}
	if store.nextRunCalls[0].nextRunAt != nil {
		t.Error("one-shot entry nextRunAt should be nil")
	}

	// Message published
	if len(transport.messages) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(transport.messages))
	}
}

func TestTick_SyntheticMessageFields(t *testing.T) {
	store := &mockStore{entries: []entity.CronEntry{dueEntry("recurring")}}
	transport := &mockTransport{}
	s := New(store, transport, nil, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	if len(transport.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(transport.messages))
	}
	msg := transport.messages[0]

	// Identity fields at Message root
	if msg.TenantID != "t1" {
		t.Errorf("expected tenant t1, got %s", msg.TenantID)
	}
	if msg.UserID != "u1" {
		t.Errorf("expected user u1, got %s", msg.UserID)
	}
	// Chat-centric routing fields
	if msg.ChatID != "chat-1" {
		t.Errorf("expected ChatID=chat-1, got %s", msg.ChatID)
	}
	if msg.IsGroup {
		t.Error("cron session should not be a group")
	}
	// Recurring entries get a fresh ConversationID each firing, distinct from the
	// conversation stored on the entry.
	if msg.ConversationID.IsEmpty() {
		t.Error("expected non-empty ConversationID for recurring firing")
	}
	if msg.ConversationID == "conv-orig-1" {
		t.Errorf("recurring firing should get a fresh ConversationID, got stored %s", msg.ConversationID)
	}

	// Message fields
	if msg.Role != entity.RoleUser {
		t.Errorf("expected role user, got %s", msg.Role)
	}
	if msg.Kind != entity.MessageKindCron {
		t.Errorf("expected kind cron, got %s", msg.Kind)
	}
	if msg.Content != "Generate daily report" {
		t.Errorf("unexpected content: %s", msg.Content)
	}
	if msg.AgentID != "a1" {
		t.Errorf("expected agent a1, got %s", msg.AgentID)
	}

	// Metadata propagated from entry
	if msg.Metadata == nil {
		t.Fatal("expected Metadata, got nil")
	}
	if msg.Metadata["sender_name"] != "Test User" {
		t.Errorf("Metadata[sender_name] = %q, want %q", msg.Metadata["sender_name"], "Test User")
	}
	if msg.Metadata["cron_name"] != "daily report" {
		t.Errorf("Metadata[cron_name] = %q, want %q", msg.Metadata["cron_name"], "daily report")
	}
}

func TestTick_ChatIDPassedFromEntry(t *testing.T) {
	store := &mockStore{entries: []entity.CronEntry{dueEntry("recurring")}}
	transport := &mockTransport{}
	s := New(store, transport, nil, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	msg := transport.messages[0]
	if msg.ChatID != "chat-1" {
		t.Errorf("expected ChatID=chat-1, got %q", msg.ChatID)
	}
}

func TestTick_RoutingFieldsPropagated(t *testing.T) {
	entry := dueEntry("recurring")
	entry.ChatID = "chat-group-99"
	entry.IsGroup = true
	entry.ConversationID = "conv-abc"
	store := &mockStore{entries: []entity.CronEntry{entry}}
	transport := &mockTransport{}
	s := New(store, transport, nil, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	if len(transport.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(transport.messages))
	}
	msg := transport.messages[0]
	if msg.ChatID != "chat-group-99" {
		t.Errorf("ChatID = %q, want %q", msg.ChatID, "chat-group-99")
	}
	if !msg.IsGroup {
		t.Error("expected IsGroup=true")
	}
	// Recurring entries get a fresh ConversationID each firing, distinct from
	// entry.ConversationID ("conv-abc").
	if msg.ConversationID.IsEmpty() {
		t.Error("expected non-empty ConversationID for recurring firing")
	}
	if msg.ConversationID == "conv-abc" {
		t.Errorf("recurring firing should get a fresh ConversationID, got stored %q", msg.ConversationID)
	}
}

func TestTick_PublishError_CronRunUpdatedToFailed(t *testing.T) {
	store := &mockStore{entries: []entity.CronEntry{dueEntry("recurring")}}
	transport := &mockTransport{err: errors.New("publish failed")}
	s := New(store, transport, nil, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	// CronRun should be updated to failed
	if len(store.updatedRuns) != 1 {
		t.Fatalf("expected 1 updated run, got %d", len(store.updatedRuns))
	}
	ur := store.updatedRuns[0]
	if ur.status != "failed" {
		t.Errorf("expected failed status, got %s", ur.status)
	}
	if ur.errMsg != "publish failed" {
		t.Errorf("unexpected error message: %s", ur.errMsg)
	}
	if ur.finishedAt == nil {
		t.Error("finishedAt should be set on failure")
	}
}

func TestTick_FutureEntryNotFired(t *testing.T) {
	entry := dueEntry("recurring")
	future := fixedNow().Add(time.Hour)
	entry.NextRunAt = &future
	store := &mockStore{entries: []entity.CronEntry{entry}}
	transport := &mockTransport{}
	s := New(store, transport, nil, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	if len(transport.messages) != 0 {
		t.Errorf("expected 0 messages for future entry, got %d", len(transport.messages))
	}
}

func TestTick_NilNextRunAtNotFired(t *testing.T) {
	entry := dueEntry("recurring")
	entry.NextRunAt = nil
	store := &mockStore{entries: []entity.CronEntry{entry}}
	transport := &mockTransport{}
	s := New(store, transport, nil, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	if len(transport.messages) != 0 {
		t.Errorf("expected 0 messages for nil NextRunAt, got %d", len(transport.messages))
	}
}

func TestStartStop(t *testing.T) {
	store := &mockStore{}
	transport := &mockTransport{}
	s := New(store, transport, nil, 50*time.Millisecond, "UTC")
	ctx := context.Background()
	s.Start(ctx)

	// Let it tick at least once
	time.Sleep(100 * time.Millisecond)

	s.Stop()

	// Should not panic or hang -- Stop waits for goroutine exit
}

func TestStop_NilCancel(t *testing.T) {
	store := &mockStore{}
	transport := &mockTransport{}
	s := New(store, transport, nil, time.Minute, "UTC")

	// Stop without Start should not panic
	s.Stop()
}

func TestTick_CronRunInserted(t *testing.T) {
	store := &mockStore{entries: []entity.CronEntry{dueEntry("recurring")}}
	transport := &mockTransport{}
	s := New(store, transport, nil, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	if len(store.insertedRuns) != 1 {
		t.Fatalf("expected 1 cron run, got %d", len(store.insertedRuns))
	}
	run := store.insertedRuns[0]
	if run.CronEntryID != "cron-1" {
		t.Errorf("expected entry id cron-1, got %s", run.CronEntryID)
	}
	if run.TenantID != "t1" {
		t.Errorf("expected tenant t1, got %s", run.TenantID)
	}
	if run.Status != "running" {
		t.Errorf("expected status running, got %s", run.Status)
	}
}

func TestTick_AttachmentFilesCopied(t *testing.T) {
	entry := dueEntry("once")
	entry.MessageID = "orig-msg-1"

	// Create a real source file to copy from.
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "photo.png")
	srcContent := []byte("fake png content")
	if err := os.WriteFile(srcPath, srcContent, 0o644); err != nil {
		t.Fatal(err)
	}

	origMsg := entity.Message{
		ID: "orig-msg-1",
		Attachments: []entity.Attachment{
			{ID: "att-1", Kind: "photo", Filename: "photo.png", MimeType: "image/png", Size: int64(len(srcContent)), Path: srcPath},
		},
	}

	store := &mockStore{
		entries:  []entity.CronEntry{entry},
		messages: map[entity.MessageID]entity.Message{"orig-msg-1": origMsg},
	}
	transport := &mockTransport{}
	sfs := newMockScopedFS(t)
	s := New(store, transport, sfs, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	if len(transport.messages) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(transport.messages))
	}
	msg := transport.messages[0]
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}

	att := msg.Attachments[0]
	if att.MimeType != "image/png" {
		t.Errorf("expected mime image/png, got %s", att.MimeType)
	}
	// Path should be different from the original (new copy).
	if att.Path == srcPath {
		t.Errorf("expected copied path to differ from source %s", srcPath)
	}
	// Verify the copied file exists and has correct content.
	got, err := os.ReadFile(att.Path)
	if err != nil {
		t.Fatalf("failed to read copied file %s: %v", att.Path, err)
	}
	if string(got) != string(srcContent) {
		t.Errorf("copied content = %q, want %q", got, srcContent)
	}
}

func TestTick_GetMessagesError_StillFires(t *testing.T) {
	entry := dueEntry("once")
	entry.MessageID = "orig-msg-1"

	store := &mockStore{
		entries:        []entity.CronEntry{entry},
		getMessagesErr: errors.New("db error"),
	}
	transport := &mockTransport{}
	sfs := newMockScopedFS(t)
	s := New(store, transport, sfs, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	if len(transport.messages) != 1 {
		t.Fatalf("expected 1 published message despite GetMessages error, got %d", len(transport.messages))
	}
	if len(transport.messages[0].Attachments) != 0 {
		t.Errorf("expected no attachments on error, got %d", len(transport.messages[0].Attachments))
	}
}

func TestTick_AttachmentCopyFailure_StillFires(t *testing.T) {
	entry := dueEntry("once")
	entry.MessageID = "orig-msg-1"

	origMsg := entity.Message{
		ID: "orig-msg-1",
		Attachments: []entity.Attachment{
			{ID: "att-1", Kind: "photo", Filename: "photo.png", MimeType: "image/png", Path: "/nonexistent/photo.png"},
		},
	}

	store := &mockStore{
		entries:  []entity.CronEntry{entry},
		messages: map[entity.MessageID]entity.Message{"orig-msg-1": origMsg},
	}
	transport := &mockTransport{}
	sfs := newMockScopedFS(t)
	s := New(store, transport, sfs, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	// Message should still be published even when file copy fails.
	if len(transport.messages) != 1 {
		t.Fatalf("expected 1 published message despite copy failure, got %d", len(transport.messages))
	}
	if len(transport.messages[0].Attachments) != 0 {
		t.Errorf("expected no attachments when source file missing, got %d", len(transport.messages[0].Attachments))
	}
}

func TestTick_NilScopedFS_SkipsAttachments(t *testing.T) {
	entry := dueEntry("once")
	entry.MessageID = "orig-msg-1"

	origMsg := entity.Message{
		ID: "orig-msg-1",
		Attachments: []entity.Attachment{
			{ID: "att-1", Kind: "photo", Filename: "photo.png", MimeType: "image/png", Path: "/tmp/photo.png"},
		},
	}

	store := &mockStore{
		entries:  []entity.CronEntry{entry},
		messages: map[entity.MessageID]entity.Message{"orig-msg-1": origMsg},
	}
	transport := &mockTransport{}
	// nil ScopedFS — attachment copy should be skipped entirely.
	s := New(store, transport, nil, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	if len(transport.messages) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(transport.messages))
	}
	if len(transport.messages[0].Attachments) != 0 {
		t.Errorf("expected no attachments with nil ScopedFS, got %d", len(transport.messages[0].Attachments))
	}
}

func TestTick_OneShotPreservesConversationID(t *testing.T) {
	entry := dueEntry("once")
	entry.ConversationID = "conv-once-1"
	store := &mockStore{entries: []entity.CronEntry{entry}}
	transport := &mockTransport{}
	s := New(store, transport, nil, time.Minute, "UTC")
	s.tick(context.Background(), fixedNow())

	if len(transport.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(transport.messages))
	}
	if got := transport.messages[0].ConversationID; got != "conv-once-1" {
		t.Errorf("one-shot firing should preserve entry.ConversationID, got %q want %q", got, "conv-once-1")
	}
}

func TestTick_RecurringFreshConversationEachFiring(t *testing.T) {
	store := &mockStore{entries: []entity.CronEntry{dueEntry("recurring")}}
	transport := &mockTransport{}
	s := New(store, transport, nil, time.Minute, "UTC")

	// Two ticks back-to-back: the mock store does not advance NextRunAt on the
	// returned entry, so both ticks fire the same recurring entry.
	s.tick(context.Background(), fixedNow())
	s.tick(context.Background(), fixedNow())

	if len(transport.messages) != 2 {
		t.Fatalf("expected 2 messages across two firings, got %d", len(transport.messages))
	}
	a, b := transport.messages[0].ConversationID, transport.messages[1].ConversationID
	if a.IsEmpty() || b.IsEmpty() {
		t.Fatalf("both firings must have non-empty ConversationID, got %q and %q", a, b)
	}
	if a == b {
		t.Errorf("recurring firings must produce distinct ConversationIDs, got %q twice", a)
	}
	if a == "conv-orig-1" || b == "conv-orig-1" {
		t.Errorf("recurring firings must not reuse entry.ConversationID %q, got (%q, %q)", "conv-orig-1", a, b)
	}
}
