// Package cli implements CLI management commands for whiteagent.
// Commands access the database by loading config.json, initializing the store
// plugin only, then calling StorePlugin methods directly.
package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/app/config"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/infra/loader"
)

// initStore loads the config file and bootstraps only the store plugin.
// It returns the StorePlugin, a cleanup function (calls mgr.Stop), and any error.
func initStore(configPath string) (port.StorePlugin, func(), error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	ldr := loader.NewLoader()
	registry, err := ldr.Load([]loader.PluginEntry{{
		Path:         cfg.Store.Path,
		ExpectedKind: entity.PluginKindStore,
		Config:       cfg.Store.Config,
		ID:           "store." + cfg.Store.PluginID,
	}})
	if err != nil {
		return nil, nil, fmt.Errorf("load store plugin: %w", err)
	}

	mgr := loader.NewManager(registry)
	ctx := context.Background()
	if err := mgr.Init(ctx, cfg.ConfigsByID()); err != nil {
		return nil, nil, fmt.Errorf("init store plugin: %w", err)
	}

	store, ok := registry.Get("store." + cfg.Store.PluginID).(port.StorePlugin)
	if !ok {
		_ = mgr.Stop(ctx)
		return nil, nil, fmt.Errorf("store plugin %q not found or wrong type", cfg.Store.PluginID)
	}

	cleanup := func() { _ = mgr.Stop(ctx) }
	return store, cleanup, nil
}

// parseExpiry parses a duration string with support for "Nd" (days) notation.
// Falls back to time.ParseDuration for standard Go duration strings ("1h", "30m", etc.).
func parseExpiry(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid duration: %s", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// fatalf prints a formatted error message to stderr and exits with code 1.
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
