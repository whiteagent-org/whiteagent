package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// RunUser dispatches user subcommands: list, remove.
func RunUser(args []string) {
	if len(args) < 1 {
		printUserUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		userList(args[1:])
	case "remove":
		userRemove(args[1:])
	default:
		printUserUsage()
		os.Exit(1)
	}
}

func printUserUsage() {
	fmt.Fprintln(os.Stderr, "Usage: whiteagent user <action> [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Actions:")
	fmt.Fprintln(os.Stderr, "  list     List users for a tenant")
	fmt.Fprintln(os.Stderr, "  remove   Soft-delete a user from a tenant")
}

func userList(args []string) {
	fs := flag.NewFlagSet("user list", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	tenantID := fs.String("tenant", "", "tenant ID (required)")
	fs.Parse(args)

	if *tenantID == "" {
		fatalf("--tenant is required")
	}

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	users, err := store.ListUsers(ctx, entity.TenantID(*tenantID))
	if err != nil {
		fatalf("list users: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPREFERRED CHANNEL\tCREATED")
	for _, u := range users {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			u.ID, u.Name, u.PreferredChannel, u.CreatedAt.Format(time.RFC3339))
	}
	w.Flush()
}

func userRemove(args []string) {
	fs := flag.NewFlagSet("user remove", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	tenantID := fs.String("tenant", "", "tenant ID (required)")
	fs.Parse(args)

	if *tenantID == "" {
		fatalf("--tenant is required")
	}
	if fs.NArg() < 1 {
		fatalf("usage: whiteagent user remove --tenant <id> [--config path] <user-id>")
	}
	userID := entity.UserID(fs.Arg(0))

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	if err := store.DeleteUser(ctx, entity.TenantID(*tenantID), userID); err != nil {
		fatalf("remove user: %v", err)
	}

	fmt.Printf("User removed: %s\n", userID)
}
