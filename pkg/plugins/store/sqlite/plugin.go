// Package sqlite implements the StorePlugin interface backed by SQLite.
// Uses modernc.org/sqlite (pure Go, no CGo) so the plugin builds cleanly as a .so.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/logger"
)

const (
	pluginID = "store.sqlite"
)

// config is the JSON shape expected in the plugin's config block.
// Only "path" is exposed — pragmas and pool settings are hardcoded best-practice defaults.
type config struct {
	Path string `json:"path"`
}

// Plugin implements port.StorePlugin backed by a SQLite database.
type Plugin struct {
	id  string
	db  *sql.DB
	cfg config
}

// Manifest is exported as the Manifest symbol looked up by the loader before NewPlugin.
// Returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{
		Kind: entity.PluginKindStore,
	}
}

// NewPlugin is the exported symbol the loader calls to obtain the plugin instance.
// Returns port.Plugin; the loader performs kind-specific interface assertion after load.
func NewPlugin() port.Plugin {
	return &Plugin{id: pluginID}
}

// ID returns the plugin identifier.
func (p *Plugin) ID() string { return p.id }

// Kind returns the plugin kind.
func (p *Plugin) Kind() entity.PluginKind { return entity.PluginKindStore }

// Init parses config, opens the SQLite database, and runs schema migrations.
// Fails fast on any error — runtime will not start with a misconfigured store.
func (p *Plugin) Init(ctx context.Context, id string, raw json.RawMessage) error {
	if id != "" {
		p.id = id
	}
	ctx = logger.WithComponent(ctx, "store.sqlite")
	log := logger.FromCtx(ctx)

	if len(raw) == 0 {
		return fmt.Errorf("store.sqlite: config is required (provide at minimum {\"path\": \"...\"})")
	}

	if err := json.Unmarshal(raw, &p.cfg); err != nil {
		return fmt.Errorf("store.sqlite: parse config: %w", err)
	}
	if p.cfg.Path == "" {
		return fmt.Errorf("store.sqlite: config.path must not be empty")
	}

	db, err := openDB(p.cfg.Path)
	if err != nil {
		return fmt.Errorf("store.sqlite: open db: %w", err)
	}
	p.db = db

	log.Info("running schema migrations", "db_path", p.cfg.Path)
	if err := p.MigrateToLatest(ctx); err != nil {
		_ = p.db.Close()
		return fmt.Errorf("store.sqlite: migrate: %w", err)
	}
	log.Info("store plugin initialized", "db_path", p.cfg.Path)
	return nil
}

// Start is a no-op for SQLite — the DB is already open after Init.
func (p *Plugin) Start(_ context.Context) error { return nil }

// Status returns the plugin health state.
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

// Stop closes the database connection.
func (p *Plugin) Stop(_ context.Context) error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// openDB opens the SQLite database at path with WAL mode and best-practice pragmas.
// Driver name: "sqlite" (modernc.org/sqlite — NOT "sqlite3" which is mattn/go-sqlite3).
// Pragma syntax: _pragma=name(value) — NOT _journal_mode=WAL (that is mattn syntax).
func openDB(path string) (*sql.DB, error) {
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=cache_size(-64000)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=temp_store(MEMORY)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Single writer: SQLite allows only one concurrent writer. SetMaxOpenConns(1)
	// serializes all writes through database/sql's pool, eliminating SQLITE_BUSY errors.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	return db, nil
}
