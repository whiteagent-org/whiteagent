package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// RunTenantMapping dispatches tenant-mapping subcommands: list, delete.
func RunTenantMapping(args []string) {
	if len(args) < 1 {
		printTenantMappingUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		tenantMappingList(args[1:])
	case "delete":
		tenantMappingDelete(args[1:])
	default:
		printTenantMappingUsage()
		os.Exit(1)
	}
}

func printTenantMappingUsage() {
	fmt.Fprintln(os.Stderr, "Usage: whiteagent tenant-mapping <action> [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Actions:")
	fmt.Fprintln(os.Stderr, "  list     List tenant mappings (optional --tenant filter)")
	fmt.Fprintln(os.Stderr, "  delete   Delete a tenant mapping (--channel, --workspace required)")
}

func tenantMappingList(args []string) {
	fs := flag.NewFlagSet("tenant-mapping list", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	tenantID := fs.String("tenant", "", "filter by tenant ID")
	fs.Parse(args)

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	mappings, err := store.ListTenantMappings(ctx, entity.TenantID(*tenantID))
	if err != nil {
		fatalf("list tenant mappings: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CHANNEL\tWORKSPACE\tTENANT")
	for _, m := range mappings {
		fmt.Fprintf(w, "%s\t%s\t%s\n", m.ChannelID, m.ExternalTenantID, string(m.TenantID))
	}
	w.Flush()
}

func tenantMappingDelete(args []string) {
	fs := flag.NewFlagSet("tenant-mapping delete", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	channelID := fs.String("channel", "", "channel plugin ID (required)")
	workspaceID := fs.String("workspace", "", "external workspace ID (required)")
	fs.Parse(args)

	if *channelID == "" {
		fatalf("--channel is required")
	}
	if *workspaceID == "" {
		fatalf("--workspace is required")
	}

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	if err := store.DeleteTenantMapping(ctx, *channelID, *workspaceID); err != nil {
		fatalf("delete tenant mapping: %v", err)
	}

	fmt.Printf("Tenant mapping deleted: %s/%s\n", *channelID, *workspaceID)
}
