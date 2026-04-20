package file_read

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mock sandbox (multi-call dispatch)
// ---------------------------------------------------------------------------

type mockSandbox struct {
	port.SandboxPlugin // embed to satisfy Plugin sub-interface stubs
	calls              []port.ExecRequest
	results            []port.ExecResult
	errors             []error
	callIdx            int
}

func (m *mockSandbox) ID() string                                                { return "mock-sandbox" }
func (m *mockSandbox) Kind() entity.PluginKind                                   { return entity.PluginKindSandbox }
func (m *mockSandbox) Status() entity.PluginState                                { return entity.PluginStateHealthy }
func (m *mockSandbox) Init(_ context.Context, _ string, _ json.RawMessage) error { return nil }
func (m *mockSandbox) Start(_ context.Context) error                             { return nil }
func (m *mockSandbox) Stop(_ context.Context) error                              { return nil }
func (m *mockSandbox) Ensure(_ context.Context, _ entity.UserID) (string, error) {
	return "/home/whiteagent", nil
}
func (m *mockSandbox) Exec(_ context.Context, _ entity.UserID, req port.ExecRequest) (port.ExecResult, error) {
	m.calls = append(m.calls, req)
	idx := m.callIdx
	m.callIdx++
	var res port.ExecResult
	var err error
	if idx < len(m.results) {
		res = m.results[idx]
	}
	if idx < len(m.errors) {
		err = m.errors[idx]
	}
	return res, err
}
func (m *mockSandbox) Release(_ context.Context, _ entity.UserID) error { return nil }
func (m *mockSandbox) UserHomePath(_ entity.UserID) string              { return "/home/whiteagent" }
func (m *mockSandbox) TenantHomePath(_ entity.TenantID) string          { return "/tenant" }
func (m *mockSandbox) MessagesPath() string                             { return "/messages" }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newPlugin(t *testing.T, rawCfg json.RawMessage) *Plugin {
	t.Helper()
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "tool.file_read", rawCfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return p
}

func tc() port.ToolContext {
	return port.ToolContext{
		TenantID: "tenant1",
		UserID:   "user1",
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestFileReadPagination(t *testing.T) {
	p := newPlugin(t, nil)
	sb := &mockSandbox{
		results: []port.ExecResult{
			{Stdout: "5 /home/whiteagent/file.txt\n", ExitCode: 0}, // wc -l
			{Stdout: "line2\nline3\nline4\n", ExitCode: 0},         // sed
		},
		errors: []error{nil, nil},
	}
	p.SetSandbox(sb)

	result, err := p.Execute(context.Background(), tc(), json.RawMessage(`{"path":"/home/whiteagent/file.txt","offset":2,"limit":3}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Verify line number format: %6d\t
	if !strings.Contains(result, "     2\tline2") {
		t.Errorf("expected line 2 with number prefix, got %q", result)
	}
	if !strings.Contains(result, "     3\tline3") {
		t.Errorf("expected line 3 with number prefix, got %q", result)
	}
	if !strings.Contains(result, "     4\tline4") {
		t.Errorf("expected line 4 with number prefix, got %q", result)
	}

	// Verify footer
	if !strings.Contains(result, "--- 3 lines (2-4 of 5) ---") {
		t.Errorf("expected footer '--- 3 lines (2-4 of 5) ---', got %q", result)
	}

	// Verify wc call
	if len(sb.calls) < 1 || sb.calls[0].Command != "wc" {
		t.Errorf("expected first call to be wc, got %v", sb.calls)
	}
	// Verify sed call
	if len(sb.calls) < 2 || sb.calls[1].Command != "sed" {
		t.Errorf("expected second call to be sed, got %v", sb.calls)
	}
}

func TestFileReadDefaultLimit(t *testing.T) {
	p := newPlugin(t, nil)
	sb := &mockSandbox{
		results: []port.ExecResult{
			{Stdout: "3 /home/whiteagent/file.txt\n", ExitCode: 0},
			{Stdout: "a\nb\nc\n", ExitCode: 0},
		},
		errors: []error{nil, nil},
	}
	p.SetSandbox(sb)

	result, err := p.Execute(context.Background(), tc(), json.RawMessage(`{"path":"/home/whiteagent/file.txt"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Default offset=1, default limit=1000, but file has only 3 lines
	// sed range should be "1,1000p" (sed handles past-end gracefully)
	if len(sb.calls) < 2 {
		t.Fatalf("expected 2 sandbox calls, got %d", len(sb.calls))
	}
	sedArgs := sb.calls[1].Args
	if len(sedArgs) < 2 || sedArgs[1] != "1,1000p" {
		t.Errorf("expected sed args with default limit, got %v", sedArgs)
	}

	// Output starts from line 1
	if !strings.Contains(result, "     1\ta") {
		t.Errorf("expected line 1 with number prefix, got %q", result)
	}

	// Footer reflects actual lines returned
	if !strings.Contains(result, "--- 3 lines (1-3 of 3) ---") {
		t.Errorf("expected footer '--- 3 lines (1-3 of 3) ---', got %q", result)
	}
}

func TestFileReadFooter(t *testing.T) {
	p := newPlugin(t, nil)
	sb := &mockSandbox{
		results: []port.ExecResult{
			{Stdout: "10 /home/whiteagent/file.txt\n", ExitCode: 0},
			{Stdout: "x\ny\n", ExitCode: 0},
		},
		errors: []error{nil, nil},
	}
	p.SetSandbox(sb)

	result, err := p.Execute(context.Background(), tc(), json.RawMessage(`{"path":"/home/whiteagent/file.txt","offset":9,"limit":5}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// sed returns 2 lines (lines 9-10), even though limit was 5
	if !strings.Contains(result, "--- 2 lines (9-10 of 10) ---") {
		t.Errorf("expected footer '--- 2 lines (9-10 of 10) ---', got %q", result)
	}
}

func TestFileReadOffsetBeyondEnd(t *testing.T) {
	p := newPlugin(t, nil)
	sb := &mockSandbox{
		results: []port.ExecResult{
			{Stdout: "5 /home/whiteagent/file.txt\n", ExitCode: 0},
		},
		errors: []error{nil},
	}
	p.SetSandbox(sb)

	result, err := p.Execute(context.Background(), tc(), json.RawMessage(`{"path":"/home/whiteagent/file.txt","offset":100}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(result, "exceeds") {
		t.Errorf("expected 'exceeds' in result, got %q", result)
	}
	if !strings.Contains(result, "5 lines") {
		t.Errorf("expected '5 lines' in result, got %q", result)
	}
}

func TestFileReadParameters(t *testing.T) {
	p := newPlugin(t, nil)
	params := p.Parameters()

	s := string(params)
	if !strings.Contains(s, `"offset"`) {
		t.Errorf("expected 'offset' in parameters, got %s", s)
	}
	if !strings.Contains(s, `"limit"`) {
		t.Errorf("expected 'limit' in parameters, got %s", s)
	}
	if !strings.Contains(s, `"path"`) {
		t.Errorf("expected 'path' in parameters, got %s", s)
	}
	// path is required, offset/limit are not
	if !strings.Contains(s, `"required":["path"]`) {
		t.Errorf("expected only path in required, got %s", s)
	}
}

func TestFileReadInitWithLinesLimit(t *testing.T) {
	p := NewPlugin().(*Plugin)
	err := p.Init(context.Background(), "tool.file_read", json.RawMessage(`{"lines_limit":500}`))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.linesLimit != 500 {
		t.Errorf("expected linesLimit 500, got %d", p.linesLimit)
	}
}

func TestFileReadInitDefaultLinesLimit(t *testing.T) {
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "tool.file_read", nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.linesLimit != 1000 {
		t.Errorf("expected default linesLimit 1000, got %d", p.linesLimit)
	}
}

func TestFileReadLineNumberFormat(t *testing.T) {
	p := newPlugin(t, nil)
	sb := &mockSandbox{
		results: []port.ExecResult{
			{Stdout: "2 /home/whiteagent/file.txt\n", ExitCode: 0},
			{Stdout: "hello\nworld\n", ExitCode: 0},
		},
		errors: []error{nil, nil},
	}
	p.SetSandbox(sb)

	result, err := p.Execute(context.Background(), tc(), json.RawMessage(`{"path":"/home/whiteagent/file.txt"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// fmt.Sprintf("%6d\t%s", lineNum, content) format
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 output lines, got %d", len(lines))
	}
	if lines[0] != "     1\thello" {
		t.Errorf("expected '     1\\thello', got %q", lines[0])
	}
	if lines[1] != "     2\tworld" {
		t.Errorf("expected '     2\\tworld', got %q", lines[1])
	}
}

func TestFileReadNonZeroExitOnWc(t *testing.T) {
	p := newPlugin(t, nil)
	sb := &mockSandbox{
		results: []port.ExecResult{
			{Stderr: "permission denied", ExitCode: 1},
		},
		errors: []error{nil},
	}
	p.SetSandbox(sb)

	result, err := p.Execute(context.Background(), tc(), json.RawMessage(`{"path":"/home/whiteagent/file.txt"}`))
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !strings.Contains(result, "permission denied") {
		t.Errorf("expected error message in result, got %q", result)
	}
}

func TestFileReadNonZeroExitOnSed(t *testing.T) {
	p := newPlugin(t, nil)
	sb := &mockSandbox{
		results: []port.ExecResult{
			{Stdout: "5 /home/whiteagent/file.txt\n", ExitCode: 0},
			{Stderr: "sed: error", ExitCode: 1},
		},
		errors: []error{nil, nil},
	}
	p.SetSandbox(sb)

	result, err := p.Execute(context.Background(), tc(), json.RawMessage(`{"path":"/home/whiteagent/file.txt"}`))
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !strings.Contains(result, "sed: error") {
		t.Errorf("expected error message in result, got %q", result)
	}
}

func TestFileReadNilSandbox(t *testing.T) {
	p := newPlugin(t, nil)
	// sandbox intentionally not set

	_, err := p.Execute(context.Background(), tc(), json.RawMessage(`{"path":"/home/whiteagent/file.txt"}`))
	if err == nil {
		t.Fatal("expected error when sandbox is nil")
	}
	if !strings.Contains(err.Error(), "sandbox") {
		t.Errorf("expected 'sandbox' in error message, got %q", err.Error())
	}
}

func TestFileReadPathRequired(t *testing.T) {
	p := newPlugin(t, nil)
	sb := &mockSandbox{
		results: []port.ExecResult{},
		errors:  []error{},
	}
	p.SetSandbox(sb)

	_, err := p.Execute(context.Background(), tc(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error when path is missing")
	}
	if !strings.Contains(err.Error(), "path") {
		t.Errorf("expected 'path' in error message, got %q", err.Error())
	}
}
