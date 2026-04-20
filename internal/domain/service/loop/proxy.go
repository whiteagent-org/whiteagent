package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/secret"
)

// secretEnvProvider is a narrow interface for secret environment variable
// resolution, extracted from secret.SecretService for testability.
type secretEnvProvider interface {
	EnvVars(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) ([]secret.SecretEnvEntry, error)
}

// sandboxProxy wraps a real SandboxPlugin and auto-injects secrets, mounts,
// and the conversation messages directory into every container. The messages
// directory is bind-mounted read-only so tools can access attachments without
// per-message CopyTo transfers.
type sandboxProxy struct {
	real           port.SandboxPlugin
	secretSvc      secretEnvProvider
	scopedFS       port.ScopedFS
	tenantID       entity.TenantID
	userID         entity.UserID
	outMsgID       entity.MessageID
	convID         entity.ConversationID
	messagesDir    string // host path for conversation messages dir (always set when scopedFS available)
	messagesTarget string // container mount target (e.g. "/messages")
	userHome       string
	tenantHome     string
}

// newSandboxProxy creates a proxy that wraps the real sandbox plugin.
// It resolves user and tenant home paths from ScopedFS at creation time.
// messagesDir is the host path to the conversation messages directory;
// messagesTarget is the container mount point (e.g. "/messages").
func newSandboxProxy(real port.SandboxPlugin, secretSvc secretEnvProvider, scopedFS port.ScopedFS, tenantID entity.TenantID, userID entity.UserID, outMsgID entity.MessageID, convID entity.ConversationID, messagesDir, messagesTarget string) (*sandboxProxy, error) {
	userHome, err := scopedFS.EnsureDir(port.ScopeUser, string(userID))
	if err != nil {
		return nil, err
	}
	tenantHome, err := scopedFS.EnsureDir(port.ScopeTenant, string(tenantID))
	if err != nil {
		return nil, err
	}
	// Always compute messagesDir so the container mount and HarvestOutgoing
	// use the correct tenant/conversation path structure.
	if messagesDir == "" {
		convDir, dirErr := scopedFS.GetDir(port.ScopeMessage, filepath.Join(string(tenantID), string(convID)))
		if dirErr == nil {
			messagesDir = convDir
		}
	}
	return &sandboxProxy{
		real:           real,
		secretSvc:      secretSvc,
		scopedFS:       scopedFS,
		tenantID:       tenantID,
		userID:         userID,
		outMsgID:       outMsgID,
		convID:         convID,
		messagesDir:    messagesDir,
		messagesTarget: messagesTarget,
		userHome:       userHome,
		tenantHome:     tenantHome,
	}, nil
}

// --- SandboxPlugin interface ---

func (p *sandboxProxy) Ensure(ctx context.Context, userID entity.UserID) (string, error) {
	var workDir string
	var err error
	if me, ok := p.real.(port.MountEnsurer); ok {
		mounts := []port.Mount{
			{Source: p.userHome, Target: "/home/whiteagent", ReadOnly: false},
			{Source: p.tenantHome, Target: "/tenant", ReadOnly: true},
		}
		if p.messagesDir != "" && p.messagesTarget != "" {
			// Ensure host directory exists before mounting (Docker requires it).
			os.MkdirAll(p.messagesDir, 0o755)
			mounts = append(mounts, port.Mount{Source: p.messagesDir, Target: p.messagesTarget, ReadOnly: true})
		}
		workDir, err = me.EnsureWithMounts(ctx, userID, mounts)
	} else {
		workDir, err = p.real.Ensure(ctx, userID)
	}
	if err != nil {
		return "", err
	}

	return workDir, nil
}

func (p *sandboxProxy) Exec(ctx context.Context, userID entity.UserID, req port.ExecRequest) (port.ExecResult, error) {
	// a. Resolve secrets fresh per call.
	entries, err := p.secretSvc.EnvVars(ctx, p.tenantID, p.userID)
	if err != nil {
		slog.Warn("proxy.secrets.resolve_error", "err", err)
		// Continue without secrets rather than failing the exec.
	}

	var hasFileSecrets bool
	if len(entries) > 0 {
		if req.Env == nil {
			req.Env = make(map[string]string, len(entries))
		}
		for _, e := range entries {
			key := normalizeEnvKey(e.Key)
			if e.Mode == entity.SecretModeFile {
				hasFileSecrets = true
				req.Env[key] = "/tmp/secrets/" + key
			} else {
				req.Env[key] = e.Value
			}
		}
	}

	// b. Write file-mode secrets to temp files inside the container.
	if hasFileSecrets {
		// Create the secrets directory.
		if _, mkErr := p.real.Exec(ctx, userID, port.ExecRequest{
			Command: "mkdir",
			Args:    []string{"-p", "/tmp/secrets"},
			WorkDir: "/tmp",
		}); mkErr != nil {
			slog.Warn("proxy.secrets.mkdir_error", "err", mkErr)
		} else {
			for _, e := range entries {
				if e.Mode != entity.SecretModeFile {
					continue
				}
				key := normalizeEnvKey(e.Key)
				path := "/tmp/secrets/" + key
				// Write secret content to file via the host bind-mount.
				// The user home is bind-mounted at /home/whiteagent, so we
				// write a staging file there and move it to /tmp/secrets/.
				if _, wErr := p.real.Exec(ctx, userID, port.ExecRequest{
					Command: "sh",
					Args:    []string{"-c", fmt.Sprintf("printf '%%s' \"$_SECRET_CONTENT\" > %s && chmod 600 %s", path, path)},
					WorkDir: "/tmp",
					Env:     map[string]string{"_SECRET_CONTENT": e.Value},
				}); wErr != nil {
					slog.Warn("proxy.secrets.write_file_error", "key", key, "err", wErr)
				}
			}
		}
	}

	// c. Default work dir.
	if req.WorkDir == "" {
		req.WorkDir = "/home/whiteagent"
	}

	// d. Delegate to real sandbox.
	result, execErr := p.real.Exec(ctx, userID, req)

	// e. Clean up file-mode secrets after execution.
	if hasFileSecrets {
		if _, rmErr := p.real.Exec(ctx, userID, port.ExecRequest{
			Command: "rm",
			Args:    []string{"-rf", "/tmp/secrets"},
			WorkDir: "/tmp",
		}); rmErr != nil {
			slog.Warn("proxy.secrets.cleanup_error", "err", rmErr)
		}
	}

	return result, execErr
}

func (p *sandboxProxy) Release(ctx context.Context, userID entity.UserID) error {
	return p.real.Release(ctx, userID)
}

// PrepareOutbox creates the /home/whiteagent/.outbox/{outMsgID}/ directory inside the
// container. Since /home/whiteagent is bind-mounted, the LLM writes there directly and
// files appear on the host instantly. Each turn gets its own subdirectory
// keyed by outMsgID to support concurrent turns.
func (p *sandboxProxy) PrepareOutbox(ctx context.Context, userID entity.UserID) error {
	outboxPath := "/home/whiteagent/.outbox/" + string(p.outMsgID)
	slog.Debug("proxy.prepare_outbox", "user_id", userID, "path", outboxPath)
	_, err := p.real.Exec(ctx, userID, port.ExecRequest{
		Command: "mkdir",
		Args:    []string{"-p", outboxPath},
		WorkDir: "/home/whiteagent",
	})
	return err
}

// HarvestOutgoing renames the outbox directory to the standard message
// directory so attachment paths follow the same convention as incoming messages.
// Since /home/whiteagent is bind-mounted, files written to /home/whiteagent/.outbox/{outMsgID}/ in the
// container are at userHome/.outbox/{outMsgID}/ on the host.
func (p *sandboxProxy) HarvestOutgoing(ctx context.Context, finalMsgID entity.MessageID) (string, error) {
	msgDir := filepath.Join(p.messagesDir, string(finalMsgID))

	hostOutbox := filepath.Join(p.userHome, ".outbox", string(p.outMsgID))
	slog.Debug("proxy.harvest.start", "out_msg_id", p.outMsgID, "final_msg_id", finalMsgID, "msg_dir", msgDir, "outbox", hostOutbox)

	// Check if outbox has any files (not just exists — PrepareOutbox always creates it).
	entries, err := os.ReadDir(hostOutbox)
	if err != nil || len(entries) == 0 {
		// Clean up empty outbox dir (created by PrepareOutbox).
		os.Remove(hostOutbox)
		return msgDir, nil
	}

	// Ensure parent messages/ directory exists.
	if err := os.MkdirAll(filepath.Dir(msgDir), 0o755); err != nil {
		return "", err
	}

	// Rename outbox dir to message dir (atomic on same filesystem).
	if err := os.Rename(hostOutbox, msgDir); err != nil {
		// Cross-device fallback: copy files individually.
		slog.Debug("proxy.harvest.rename_fallback", "err", err)
		if mkErr := os.MkdirAll(msgDir, 0o755); mkErr != nil {
			return "", mkErr
		}
		entries, readErr := os.ReadDir(hostOutbox)
		if readErr != nil {
			return "", readErr
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			src := filepath.Join(hostOutbox, entry.Name())
			dst := filepath.Join(msgDir, entry.Name())
			if copyErr := copyFile(src, dst); copyErr != nil {
				slog.Warn("proxy.harvest.copy_file_error", "src", src, "dst", dst, "err", copyErr)
			}
		}
		os.RemoveAll(hostOutbox)
	}

	return msgDir, nil
}

// --- Plugin interface stubs (delegate to real) ---

func (p *sandboxProxy) ID() string                 { return p.real.ID() }
func (p *sandboxProxy) Kind() entity.PluginKind    { return p.real.Kind() }
func (p *sandboxProxy) Status() entity.PluginState { return p.real.Status() }
func (p *sandboxProxy) Init(ctx context.Context, id string, cfg json.RawMessage) error {
	return p.real.Init(ctx, id, cfg)
}
func (p *sandboxProxy) Start(ctx context.Context) error { return p.real.Start(ctx) }
func (p *sandboxProxy) Stop(ctx context.Context) error  { return p.real.Stop(ctx) }
func (p *sandboxProxy) UserHomePath(userID entity.UserID) string {
	return p.real.UserHomePath(userID)
}
func (p *sandboxProxy) TenantHomePath(tenantID entity.TenantID) string {
	return p.real.TenantHomePath(tenantID)
}
func (p *sandboxProxy) MessagesPath() string { return p.real.MessagesPath() }

// normalizeEnvKey uppercases and replaces hyphens/spaces with underscores.
func normalizeEnvKey(key string) string {
	return strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(key))
}

// copyFile copies src to dst as a fallback when os.Rename fails (cross-device).
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
