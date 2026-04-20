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

// RunTenant dispatches tenant subcommands: list, delete, update.
func RunTenant(args []string) {
	if len(args) < 1 {
		printTenantUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		tenantList(args[1:])
	case "delete":
		tenantDelete(args[1:])
	case "update":
		tenantUpdate(args[1:])
	default:
		printTenantUsage()
		os.Exit(1)
	}
}

func printTenantUsage() {
	fmt.Fprintln(os.Stderr, "Usage: whiteagent tenant <action> [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Actions:")
	fmt.Fprintln(os.Stderr, "  list     List all tenants")
	fmt.Fprintln(os.Stderr, "  delete   Soft-delete a tenant")
	fmt.Fprintln(os.Stderr, "  update   Update tenant properties")
}

func tenantList(args []string) {
	fs := flag.NewFlagSet("tenant list", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	fs.Parse(args)

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenants, err := store.ListTenants(ctx)
	if err != nil {
		fatalf("list tenants: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tJOIN POLICY\tCREATED")
	for _, t := range tenants {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			t.ID, t.Name, t.JoinPolicy, t.CreatedAt.Format(time.RFC3339))
	}
	w.Flush()
}

func tenantDelete(args []string) {
	fs := flag.NewFlagSet("tenant delete", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fatalf("usage: whiteagent tenant delete [--config path] <tenant-id>")
	}
	id := entity.TenantID(fs.Arg(0))

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	if err := store.DeleteTenant(ctx, id); err != nil {
		fatalf("delete tenant: %v", err)
	}

	fmt.Printf("Tenant deleted: %s\n", id)
}

func tenantUpdate(args []string) {
	fs := flag.NewFlagSet("tenant update", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	name := fs.String("name", "", "new tenant name")
	instructions := fs.String("instructions", "", "new instructions")
	joinPolicy := fs.String("join-policy", "", "join policy: open, invite_required, or closed")
	rejectionMessage := fs.String("rejection-message", "", "new rejection message")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fatalf("usage: whiteagent tenant update [--config path] [flags] <tenant-id>")
	}
	id := entity.TenantID(fs.Arg(0))

	// Validate join policy if provided.
	if *joinPolicy != "" && *joinPolicy != entity.JoinPolicyOpen && *joinPolicy != entity.JoinPolicyInviteRequired && *joinPolicy != entity.JoinPolicyClosed {
		fatalf("invalid join policy %q: must be open, invite_required, or closed", *joinPolicy)
	}

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant, err := store.GetTenant(ctx, id)
	if err != nil {
		fatalf("get tenant: %v", err)
	}
	if tenant == nil {
		fatalf("tenant not found: %s", id)
	}

	if *name != "" {
		tenant.Name = *name
	}
	if *instructions != "" {
		tenant.Instructions = *instructions
	}
	if *joinPolicy != "" {
		tenant.JoinPolicy = *joinPolicy
	}
	if *rejectionMessage != "" {
		tenant.RejectionMessage = *rejectionMessage
	}

	if err := store.SaveTenant(ctx, id, *tenant); err != nil {
		fatalf("save tenant: %v", err)
	}

	fmt.Printf("Tenant updated: %s\n", id)
}
