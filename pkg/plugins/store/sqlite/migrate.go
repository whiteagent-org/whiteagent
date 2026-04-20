package sqlite

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/whiteagent-org/whiteagent/pkg/logger"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// MigrateToLatest applies any unapplied SQL migrations in lexicographic filename order.
// The schema_migrations table is bootstrapped here (not via a migration file) to avoid
// a bootstrapping problem where the runner queries the table before it exists.
// Each migration is wrapped in a BEGIN/COMMIT transaction — partial failures roll back.
func (p *Plugin) MigrateToLatest(ctx context.Context) error {
	// Bootstrap: create schema_migrations table if it doesn't exist.
	// Must happen before loadAppliedVersions queries it.
	const bootstrap = `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version    TEXT PRIMARY KEY,
            applied_at DATETIME NOT NULL
        );`
	if _, err := p.db.ExecContext(ctx, bootstrap); err != nil {
		return fmt.Errorf("bootstrap schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	// Sort by filename — timestamp prefix (YYYYMMDDHHMMSS_) sorts lexicographically.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	applied, err := p.loadAppliedVersions(ctx)
	if err != nil {
		return err
	}

	log := logger.FromCtx(ctx)
	var appliedCount, skipped int

	for _, entry := range entries {
		version := entry.Name()
		if applied[version] {
			log.Debug("migration already applied", "version", version)
			skipped++
			continue
		}
		sqlText, err := fs.ReadFile(migrationFS, "migrations/"+version)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		start := time.Now()
		if err := p.applyMigration(ctx, version, string(sqlText)); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		log.Info("migration applied", "version", version, "duration", time.Since(start))
		appliedCount++
	}

	log.Info("migrations complete", "applied", appliedCount, "skipped", skipped)
	return nil
}

// loadAppliedVersions returns a set of already-applied migration filenames.
func (p *Plugin) loadAppliedVersions(ctx context.Context) (map[string]bool, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// applyMigration executes a single migration SQL file inside a transaction.
// Records the version in schema_migrations on success.
//
// Foreign key checks are disabled before the transaction and re-enabled after.
// This is the SQLite-recommended pattern for migrations that recreate tables
// (see https://www.sqlite.org/lang_altertable.html#otheralter).
// PRAGMA foreign_keys is a no-op inside a transaction, so it must be toggled
// on the connection outside BEGIN/COMMIT.
func (p *Plugin) applyMigration(ctx context.Context, version, sqlText string) error {
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return fmt.Errorf("disable foreign_keys: %w", err)
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))`,
		version); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Re-enable and verify FK integrity.
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		return fmt.Errorf("enable foreign_keys: %w", err)
	}
	rows, err := conn.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("foreign_key_check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table, rowid, parent string
		var fkid int
		_ = rows.Scan(&table, &rowid, &parent, &fkid)
		return fmt.Errorf("foreign key violation after migration: table=%s rowid=%s parent=%s", table, rowid, parent)
	}
	return rows.Err()
}
