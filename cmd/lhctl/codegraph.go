package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/divmora/localharness/internal/codegraph"
	"github.com/divmora/localharness/internal/engine"
)

func newCodeGraphCommand() *cobra.Command {
	var (
		workspaceFlag string
		branchFlag    string
	)

	cmd := &cobra.Command{
		Use:     "codegraph",
		Aliases: []string{"cg", "graph"},
		Short:   "Repository AST code graph indexing and semantic structural queries",
		Long: `codegraph manages the AST-level code graph stored in DuckDB.
It indexes symbols, call hierarchies, interfaces, and dependencies across git branches.`,
	}

	// Persistent flags across codegraph subcommands
	cmd.PersistentFlags().StringVarP(&workspaceFlag, "workspace", "w", ".", "Workspace path (default: current directory)")
	cmd.PersistentFlags().StringVarP(&branchFlag, "branch", "b", "", "Target branch (defaults to active git branch)")

	// Subcommand: index
	indexCmd := &cobra.Command{
		Use:   "index",
		Short: "Index or incrementally synchronize workspace code graph into DuckDB",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, projectID, wsPath, err := resolveCodeGraphContext(workspaceFlag)
			if err != nil {
				return err
			}

			fmt.Printf("Indexing workspace: %s (Project: %s)...\n", wsPath, projectID)
			stats, err := mgr.IndexWorkspace(context.Background(), projectID, wsPath, branchFlag)
			if err != nil {
				return fmt.Errorf("index failed: %w", err)
			}

			fmt.Printf("✓ Indexing complete in %s (Branch: %s)\n", stats.Duration, stats.Branch)
			fmt.Printf("  • Total Files:  %d (Parsed: %d, Cached: %d)\n", stats.TotalFiles, stats.ParsedFiles, stats.CachedFiles)
			fmt.Printf("  • Total Nodes:  %d\n", stats.TotalNodes)
			fmt.Printf("  • Total Edges:  %d\n", stats.TotalEdges)
			return nil
		},
	}

	// Subcommand: search
	var kindFlag string
	var limitFlag int
	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search for code symbols (functions, structs, interfaces, methods, classes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, _, err := resolveStoreContext(workspaceFlag)
			if err != nil {
				return err
			}

			query := args[0]
			scoredNodes := store.SearchSymbolsFTS(branchFlag, query, kindFlag, limitFlag)
			if len(scoredNodes) == 0 {
				fmt.Printf("No symbols found matching %q\n", query)
				return nil
			}

			fmt.Printf("Found %d symbol(s) (ranked by BM25 relevance):\n\n", len(scoredNodes))
			for _, sn := range scoredNodes {
				n := sn.Node
				fmt.Printf("  • \033[1m%s\033[0m (\033[36m%s\033[0m) [score: %.2f]\n", n.Name, n.Kind, sn.Score)
				fmt.Printf("    \033[90m%s:%d-%d\033[0m\n", n.FilePath, n.StartLine, n.EndLine)
				if n.Signature != "" {
					fmt.Printf("    \033[32m%s\033[0m\n", n.Signature)
				}
				if n.Docstring != "" {
					cleanDoc := strings.ReplaceAll(n.Docstring, "\n", " ")
					if len(cleanDoc) > 120 {
						cleanDoc = cleanDoc[:117] + "..."
					}
					fmt.Printf("    \033[33m// %s\033[0m\n", cleanDoc)
				}
				fmt.Println()
			}
			return nil
		},
	}
	searchCmd.Flags().StringVarP(&kindFlag, "kind", "k", "", "Filter by kind (function, struct, interface, method, class, type)")
	searchCmd.Flags().IntVarP(&limitFlag, "limit", "l", 50, "Maximum number of results")

	// Subcommand: impact
	var depthFlag int
	impactCmd := &cobra.Command{
		Use:   "impact <symbol-or-file>",
		Short: "Analyze blast radius, downstream callers, and affected files before refactoring",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, _, err := resolveStoreContext(workspaceFlag)
			if err != nil {
				return err
			}

			target := args[0]
			res := store.GetImpactRadius(branchFlag, target, depthFlag)

			fmt.Printf("=== Blast Radius Analysis: %s ===\n\n", target)
			fmt.Printf("Affected Files (%d):\n", len(res.AffectedFiles))
			for _, f := range res.AffectedFiles {
				fmt.Printf("  • %s\n", f)
			}

			fmt.Printf("\nDownstream Callers (%d):\n", len(res.DownstreamCallers))
			for _, c := range res.DownstreamCallers {
				fmt.Printf("  • %s (%s) in %s:%d\n", c.SymbolID, c.Kind, c.FilePath, c.StartLine)
			}

			if len(res.Implementations) > 0 {
				fmt.Printf("\nImplementations (%d):\n", len(res.Implementations))
				for _, im := range res.Implementations {
					fmt.Printf("  • %s in %s:%d\n", im.SymbolID, im.FilePath, im.StartLine)
				}
			}
			return nil
		},
	}
	impactCmd.Flags().IntVarP(&depthFlag, "depth", "d", 5, "Maximum traversal depth")

	// Subcommand: callers
	callersCmd := &cobra.Command{
		Use:   "callers <symbol>",
		Short: "Inspect incoming call hierarchy for a function/method",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, _, err := resolveStoreContext(workspaceFlag)
			if err != nil {
				return err
			}

			symbol := args[0]
			hierarchy := store.GetCallHierarchy(branchFlag, symbol, "incoming", depthFlag)
			if len(hierarchy.Calls) == 0 {
				fmt.Printf("No callers found for %q\n", symbol)
				return nil
			}

			fmt.Printf("=== Call Hierarchy for %s ===\n\n", symbol)
			for _, c := range hierarchy.Calls {
				indent := strings.Repeat("  ", c.Depth)
				fmt.Printf("%s└─ %s (in %s:%d)\n", indent, c.CallerNode.SymbolID, c.FilePath, c.Line)
			}
			return nil
		},
	}
	callersCmd.Flags().IntVarP(&depthFlag, "depth", "d", 4, "Maximum call depth")

	// Subcommand: diff
	diffCmd := &cobra.Command{
		Use:   "diff <base-branch> <target-branch>",
		Short: "Compare code graph differences between two branches",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, _, err := resolveStoreContext(workspaceFlag)
			if err != nil {
				return err
			}

			baseBranch := args[0]
			targetBranch := args[1]
			diff := store.DiffBranches(baseBranch, targetBranch)

			fmt.Printf("=== Code Graph Diff: %s ◄── %s ===\n\n", baseBranch, targetBranch)
			fmt.Printf("Added Symbols (%d):\n", len(diff.AddedNodes))
			for _, n := range diff.AddedNodes {
				fmt.Printf("  \033[32m+\033[0m %s (%s) in %s\n", n.Name, n.Kind, n.FilePath)
			}

			fmt.Printf("\nRemoved Symbols (%d):\n", len(diff.RemovedNodes))
			for _, n := range diff.RemovedNodes {
				fmt.Printf("  \033[31m-\033[0m %s (%s) in %s\n", n.Name, n.Kind, n.FilePath)
			}

			fmt.Printf("\nNew Dependencies/Calls (%d):\n", len(diff.AddedEdges))
			for _, e := range diff.AddedEdges {
				fmt.Printf("  \033[32m+\033[0m %s %s %s\n", e.SourceSymbol, e.Relation, e.TargetSymbol)
			}
			return nil
		},
	}

	// Subcommand: status
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Display code graph status, DuckDB database location, and branch counts",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, projectID, wsPath, err := resolveStoreContext(workspaceFlag)
			if err != nil {
				return err
			}

			branches := store.ListBranches()
			activeBranch := store.ActiveBranch()
			nodes := store.GetBranchNodes(activeBranch)
			edges := store.GetBranchEdges(activeBranch)

			fmt.Printf("=== LocalHarness Code Graph Status ===\n\n")
			fmt.Printf("  • Workspace:     %s\n", wsPath)
			fmt.Printf("  • Project ID:    %s\n", projectID)
			fmt.Printf("  • Database File: %s\n", store.DBPath())
			fmt.Printf("  • Active Branch: %s\n", activeBranch)
			fmt.Printf("  • Tracked Nodes: %d\n", len(nodes))
			fmt.Printf("  • Tracked Edges: %d\n", len(edges))
			fmt.Printf("  • Total Branches: %d\n", len(branches))
			for _, b := range branches {
				marker := " "
				if b.Name == activeBranch {
					marker = "*"
				}
				fmt.Printf("      %s %s\n", marker, b.Name)
			}
			return nil
		},
	}

	cmd.AddCommand(indexCmd)
	cmd.AddCommand(searchCmd)
	cmd.AddCommand(impactCmd)
	cmd.AddCommand(callersCmd)
	cmd.AddCommand(diffCmd)
	cmd.AddCommand(statusCmd)

	return cmd
}

func resolveCodeGraphContext(wsFlag string) (*codegraph.Manager, string, string, error) {
	absWS, err := filepath.Abs(wsFlag)
	if err != nil {
		return nil, "", "", fmt.Errorf("resolve workspace: %w", err)
	}

	dataDir := globalDataDir
	if dataDir == "" {
		dataDir = getDefaultDataDir()
	}

	reg := engine.NewProjectRegistry(dataDir)
	if err := reg.Load(); err != nil {
		_ = os.MkdirAll(dataDir, 0755)
	}

	proj, err := reg.FindOrCreate([]string{absWS})
	if err != nil {
		return nil, "", "", fmt.Errorf("resolve project: %w", err)
	}

	knowledgeDir := filepath.Join(dataDir, "knowledge")
	mgr := codegraph.NewManager(knowledgeDir)
	mgr.RegisterWorkspace(absWS, proj.ID)

	return mgr, proj.ID, absWS, nil
}

func resolveStoreContext(wsFlag string) (*codegraph.Store, string, string, error) {
	mgr, projID, wsPath, err := resolveCodeGraphContext(wsFlag)
	if err != nil {
		return nil, "", "", err
	}

	store, err := mgr.GetStore(projID)
	if err != nil {
		return nil, "", "", err
	}

	return store, projID, wsPath, nil
}
