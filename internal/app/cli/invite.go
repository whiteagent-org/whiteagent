package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

// RunInvite dispatches invite subcommands: create, list, revoke.
func RunInvite(args []string) {
	if len(args) < 1 {
		printInviteUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "create":
		inviteCreate(args[1:])
	case "list":
		inviteList(args[1:])
	case "revoke":
		inviteRevoke(args[1:])
	default:
		printInviteUsage()
		os.Exit(1)
	}
}

func printInviteUsage() {
	fmt.Fprintln(os.Stderr, "Usage: whiteagent invite <action> [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Actions:")
	fmt.Fprintln(os.Stderr, "  create   Generate an invite code (--type tenant|user [--target ID])")
	fmt.Fprintln(os.Stderr, "  list     List invite codes (optional --type, --tenant filters)")
	fmt.Fprintln(os.Stderr, "  revoke   Revoke an invite code")
}

func inviteCreate(args []string) {
	fs := flag.NewFlagSet("invite create", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	codeType := fs.String("type", "", "code type: tenant or user (required)")
	tenantID := fs.String("tenant", "", "tenant ID (required for type=user)")
	target := fs.String("target", "", "target entity ID for linking codes (optional)")
	fs.Parse(args)

	if *codeType != "tenant" && *codeType != "user" {
		fatalf("--type must be 'tenant' or 'user'")
	}
	if *codeType == "user" && *tenantID == "" {
		fatalf("--tenant is required for user codes")
	}

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()

	// Validate target exists if specified.
	if *target != "" {
		switch *codeType {
		case "user":
			if _, err := store.GetUser(ctx, entity.TenantID(*tenantID), entity.UserID(*target)); err != nil {
				fatalf("target user not found in tenant")
			}
		case "tenant":
			if _, err := store.GetTenant(ctx, entity.TenantID(*target)); err != nil {
				fatalf("target tenant not found")
			}
		}
	}

	code, err := util.NewInviteCode()
	if err != nil {
		fatalf("generate invite code: %v", err)
	}

	invite := entity.InviteCode{
		Code:      code,
		Type:      *codeType,
		TenantID:  entity.TenantID(*tenantID),
		TargetID:  *target,
		CreatedAt: time.Now().UTC(),
	}

	if err := store.SaveInviteCode(ctx, invite); err != nil {
		fatalf("save invite code: %v", err)
	}

	fmt.Println(code)
}

func inviteList(args []string) {
	fs := flag.NewFlagSet("invite list", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	codeType := fs.String("type", "", "filter by type: tenant or user")
	tenantID := fs.String("tenant", "", "filter by tenant ID")
	fs.Parse(args)

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	filter := entity.InviteCodeFilter{
		Type:     *codeType,
		TenantID: entity.TenantID(*tenantID),
	}
	codes, err := store.ListInviteCodes(ctx, filter)
	if err != nil {
		fatalf("list invite codes: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CODE\tTYPE\tTARGET\tSTATUS\tCREATED")
	for _, c := range codes {
		status := inviteStatus(c)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			c.Code, c.Type, c.TargetID, status, c.CreatedAt.Format(time.RFC3339))
	}
	w.Flush()
}

// inviteStatus computes the display status of an invite code.
func inviteStatus(c entity.InviteCode) string {
	if !c.UsedBy.IsEmpty() {
		return "used"
	}
	if c.RevokedAt != nil {
		return "revoked"
	}
	return "active"
}

func inviteRevoke(args []string) {
	fs := flag.NewFlagSet("invite revoke", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fatalf("usage: whiteagent invite revoke [--config path] <code>")
	}
	code := fs.Arg(0)

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	if err := store.RevokeInviteCode(ctx, code); err != nil {
		fatalf("revoke invite code: %v", err)
	}

	fmt.Printf("Invite code revoked: %s\n", code)
}
