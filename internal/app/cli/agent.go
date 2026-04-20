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

// RunAgent dispatches agent subcommands: create, list, view, update.
func RunAgent(args []string) {
	if len(args) < 1 {
		printAgentUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "create":
		agentCreate(args[1:])
	case "list":
		agentList(args[1:])
	case "view":
		agentView(args[1:])
	case "update":
		agentUpdate(args[1:])
	default:
		printAgentUsage()
		os.Exit(1)
	}
}

func printAgentUsage() {
	fmt.Fprintln(os.Stderr, "Usage: whiteagent agent <action> [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Actions:")
	fmt.Fprintln(os.Stderr, "  create   Create a new agent")
	fmt.Fprintln(os.Stderr, "  list     List agents for a tenant")
	fmt.Fprintln(os.Stderr, "  view     View agent configuration")
	fmt.Fprintln(os.Stderr, "  update   Update agent configuration")
}

func agentCreate(args []string) {
	fs := flag.NewFlagSet("agent create", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	tenantID := fs.String("tenant", "", "tenant ID (required)")
	name := fs.String("name", "", "agent name (required)")
	instructionsFlag := fs.String("instructions", "", "agent instructions")
	fs.Parse(args)

	if *tenantID == "" {
		fatalf("--tenant is required")
	}
	if *name == "" {
		fatalf("--name is required")
	}

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	instructions := *instructionsFlag
	if instructions == "" {
		instructions = entity.DefaultAgentInstructions()
	}

	agentID := entity.AgentID(util.NewID())
	agent := entity.Agent{
		ID:           agentID,
		TenantID:     entity.TenantID(*tenantID),
		Name:         *name,
		Instructions: instructions,
		CreatedAt:    time.Now().UTC(),
	}

	ctx := context.Background()
	if err := store.SaveAgent(ctx, entity.TenantID(*tenantID), agent); err != nil {
		fatalf("save agent: %v", err)
	}

	fmt.Printf("Agent created: %s (%s)\n", agentID, *name)
}

func agentList(args []string) {
	fs := flag.NewFlagSet("agent list", flag.ExitOnError)
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
	agents, err := store.ListAgents(ctx, entity.TenantID(*tenantID))
	if err != nil {
		fatalf("list agents: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tCREATED")
	for _, a := range agents {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			a.ID, a.Name, a.CreatedAt.Format(time.RFC3339))
	}
	w.Flush()
}

func agentView(args []string) {
	fs := flag.NewFlagSet("agent view", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	tenantID := fs.String("tenant", "", "tenant ID (required)")
	agentID := fs.String("agent", "", "agent ID (required)")
	fs.Parse(args)

	if *tenantID == "" {
		fatalf("--tenant is required")
	}
	if *agentID == "" {
		fatalf("--agent is required")
	}

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	ag, err := store.GetAgent(ctx, entity.TenantID(*tenantID), entity.AgentID(*agentID))
	if err != nil {
		fatalf("get agent: %v", err)
	}
	if ag == nil {
		fatalf("agent not found: %s", *agentID)
	}

	fmt.Printf("Name: %s\n", ag.Name)

	instr := ag.Instructions
	if len(instr) > 200 {
		instr = instr[:200] + "..."
	}
	fmt.Printf("Instructions: %s\n", instr)
}

func agentUpdate(args []string) {
	fs := flag.NewFlagSet("agent update", flag.ExitOnError)
	configPath := fs.String("config", "config.json", "config file path")
	tenantID := fs.String("tenant", "", "tenant ID (required)")
	agentID := fs.String("agent", "", "agent ID (required)")
	instructions := fs.String("instructions", "", "new instructions")
	fs.Parse(args)

	if *tenantID == "" {
		fatalf("--tenant is required")
	}
	if *agentID == "" {
		fatalf("--agent is required")
	}

	store, cleanup, err := initStore(*configPath)
	if err != nil {
		fatalf("init store: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	ag, err := store.GetAgent(ctx, entity.TenantID(*tenantID), entity.AgentID(*agentID))
	if err != nil {
		fatalf("get agent: %v", err)
	}
	if ag == nil {
		fatalf("agent not found: %s", *agentID)
	}

	if *instructions != "" {
		ag.Instructions = *instructions
	}

	if err := store.SaveAgent(ctx, entity.TenantID(*tenantID), *ag); err != nil {
		fatalf("save agent: %v", err)
	}

	fmt.Printf("Agent updated: %s\n", *agentID)
}
