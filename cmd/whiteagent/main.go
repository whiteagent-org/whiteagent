package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/app/agent"
	"github.com/whiteagent-org/whiteagent/internal/app/cli"
	"github.com/whiteagent-org/whiteagent/internal/app/config"
	"github.com/whiteagent-org/whiteagent/internal/app/gateway"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/logger"
	"github.com/whiteagent-org/whiteagent/pkg/runtime"
	"github.com/whiteagent-org/whiteagent/skills"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "tenant":
		cli.RunTenant(os.Args[2:])
	case "user":
		cli.RunUser(os.Args[2:])
	case "invite":
		cli.RunInvite(os.Args[2:])
	case "agent":
		cli.RunAgent(os.Args[2:])
	case "tenant-mapping":
		cli.RunTenantMapping(os.Args[2:])
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: whiteagent <command> [options] (version: %s)\n", version)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  serve     Start the agent runtime")
	fmt.Fprintln(os.Stderr, "  tenant    Manage tenants (create, list, delete, update)")
	fmt.Fprintln(os.Stderr, "  user      Manage users (list, remove)")
	fmt.Fprintln(os.Stderr, "  invite    Manage invite codes (create, list, revoke)")
	fmt.Fprintln(os.Stderr, "  agent     Manage agents (create, list, view, update)")
	fmt.Fprintln(os.Stderr, "  tenant-mapping  Manage tenant mappings (list, delete)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Note: Runtime reload is not supported. Restart the process to apply config changes.")
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	fs.Parse(args)

	// Phase 1: boot logger with INFO default before config is available.
	logger.Init(slog.LevelInfo)

	// Extract embedded global skills to ./skills/ (binary is source of truth).
	if err := skills.Extract("./skills/"); err != nil {
		slog.Error("failed to extract embedded skills", "err", err)
		os.Exit(1)
	}

	cfgPath := *configPath

	// Load and validate config.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	// Phase 2: re-init logger with configured level.
	level, err := logger.ParseLevel(cfg.Runtime.LoggingLevel)
	if err != nil {
		slog.Warn("invalid log level in config, using INFO", "err", err)
		level = slog.LevelInfo
	}
	logger.Init(level)

	slog.Info("whiteagent starting", "config", cfgPath, "version", version)

	ctx := context.Background()

	// Create runtime state and Runtime.
	state := runtime.NewRuntimeState()
	rt := agent.NewRuntime(cfg, state)

	if err := rt.Start(ctx); err != nil {
		slog.Error("failed to start runtime", "err", err)
		os.Exit(1)
	}

	// Create and start HTTP gateway.
	gw := gateway.NewGateway(cfg.Gateway.Address, rt.AllPlugins(), state.Get)
	gw.RegisterChannelRoutes(channelSlice(rt.Channels()))
	if rt.SecretService() != nil {
		gw.RegisterSecretRoutes(rt.SecretService(), func(ctx context.Context, msg entity.Message) error {
			go func() {
				if err := rt.Loop().Handle(context.Background(), msg); err != nil {
					slog.Error("secret notification callback error", "err", err)
				}
			}()
			return nil
		})
	}
	if err := gw.Start(); err != nil {
		slog.Error("failed to start gateway", "err", err)
		os.Exit(1)
	}

	state.Set(runtime.StateReady)
	slog.Info("whiteagent ready")

	// Block on signal for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("received signal, shutting down", "signal", sig)

	// Second signal forces immediate exit.
	go func() {
		<-sigCh
		slog.Warn("received second signal, forcing exit")
		os.Exit(1)
	}()

	// Parse shutdown timeout (default 30s).
	shutdownTimeout := 30 * time.Second
	if d, err := time.ParseDuration(cfg.Runtime.ShutdownTimeout); err == nil {
		shutdownTimeout = d
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	exitCode := 0

	if err := gw.Stop(shutdownCtx); err != nil {
		slog.Error("gateway shutdown error", "err", err)
	}

	if err := rt.Shutdown(shutdownCtx); err != nil {
		slog.Error("runtime shutdown error", "err", err)
		exitCode = 1
	}

	slog.Info("whiteagent stopped")
	os.Exit(exitCode)
}

// channelSlice converts a channel entry map to a slice for gateway route registration.
func channelSlice(m map[string]port.ChannelEntry) []port.ChannelEntry {
	s := make([]port.ChannelEntry, 0, len(m))
	for _, entry := range m {
		s = append(s, entry)
	}
	return s
}
