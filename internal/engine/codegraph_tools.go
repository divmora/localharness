package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/codegraph"
	"github.com/divmora/localharness/internal/llm"
)

// getCodeGraphStore resolves the Store for the active workspace.
func (e *Engine) getCodeGraphStore(tc llm.ToolCall) (*codegraph.Store, string, error) {
	if e.codeGraphManager == nil {
		return nil, "", fmt.Errorf("code graph manager not initialized")
	}

	wsPath := ""
	if len(e.workspaces) > 0 {
		wsPath = e.workspaces[0]
	}
	if wsArg, ok := tc.Args["workspace_path"].(string); ok && wsArg != "" {
		wsPath = wsArg
	}
	if wsPath == "" {
		wsPath = "."
	}

	absWS, err := filepath.Abs(wsPath)
	if err != nil {
		return nil, "", fmt.Errorf("resolve workspace path: %w", err)
	}

	project := e.projectRegistry.FindByWorkspace(absWS)
	if project == nil {
		project, err = e.projectRegistry.FindOrCreate([]string{absWS})
		if err != nil {
			return nil, "", fmt.Errorf("find/create project for workspace %s: %w", absWS, err)
		}
	}

	store, err := e.codeGraphManager.GetStore(project.ID)
	if err != nil {
		return nil, "", err
	}

	return store, absWS, nil
}

// executeCodeGraphSearch handles the codegraph_search tool.
func (e *Engine) executeCodeGraphSearch(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	store, wsPath, err := e.getCodeGraphStore(tc)
	if err != nil {
		return fmt.Errorf("codegraph_search: %w", err)
	}

	query, _ := tc.Args["query"].(string)
	kind, _ := tc.Args["kind"].(string)
	branch, _ := tc.Args["branch"].(string)
	limit := 50
	if limFloat, ok := tc.Args["limit"].(float64); ok && limFloat > 0 {
		limit = int(limFloat)
	}

	nodes := store.SearchSymbols(branch, query, kind, limit)

	var protoNodes []*pb.CodeGraphNode
	var md strings.Builder
	md.WriteString(fmt.Sprintf("### Code Graph Search: `%s`", query))
	if kind != "" {
		md.WriteString(fmt.Sprintf(" (kind: `%s`)", kind))
	}
	md.WriteString(fmt.Sprintf("\nFound %d symbol(s):\n\n", len(nodes)))

	for _, n := range nodes {
		absPath := filepath.Join(wsPath, n.FilePath)
		link := fmt.Sprintf("[%s:%d-%d](file://%s#L%d-L%d)", n.FilePath, n.StartLine, n.EndLine, absPath, n.StartLine, n.EndLine)
		md.WriteString(fmt.Sprintf("- **%s** (`%s`): %s\n", n.Name, n.Kind, link))
		if n.Signature != "" {
			md.WriteString(fmt.Sprintf("  - Signature: `%s`\n", n.Signature))
		}

		protoNodes = append(protoNodes, &pb.CodeGraphNode{
			SymbolId:  n.SymbolID,
			Kind:      n.Kind,
			Name:      n.Name,
			FilePath:  n.FilePath,
			StartLine: int32(n.StartLine),
			EndLine:   int32(n.EndLine),
			Signature: n.Signature,
			Docstring: n.Docstring,
			BlobHash:  n.BlobHash,
		})
	}

	step.Action = &pb.StepUpdate_CodeGraph{
		CodeGraph: &pb.ActionCodeGraph{
			Operation:    "search",
			Query:        query,
			Branch:       branch,
			Nodes:        protoNodes,
			Summary:      fmt.Sprintf("Found %d symbols", len(nodes)),
			TotalMatches: int32(len(nodes)),
		},
	}
	step.Text = md.String()
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	return nil
}

// executeCodeGraphFindReferences handles the codegraph_find_references tool.
func (e *Engine) executeCodeGraphFindReferences(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	store, wsPath, err := e.getCodeGraphStore(tc)
	if err != nil {
		return fmt.Errorf("codegraph_find_references: %w", err)
	}

	symbolID, _ := tc.Args["symbol_id"].(string)
	branch, _ := tc.Args["branch"].(string)

	if symbolID == "" {
		return fmt.Errorf("codegraph_find_references: symbol_id is required")
	}

	refs := store.FindReferences(branch, symbolID)

	var protoEdges []*pb.CodeGraphEdge
	var md strings.Builder
	md.WriteString(fmt.Sprintf("### References to `%s`\n", symbolID))
	md.WriteString(fmt.Sprintf("Found %d reference(s):\n\n", len(refs)))

	for _, r := range refs {
		absPath := filepath.Join(wsPath, r.FilePath)
		link := fmt.Sprintf("[%s:%d](file://%s#L%d)", r.FilePath, r.Line, absPath, r.Line)
		md.WriteString(fmt.Sprintf("- `%s` **%s** `%s` in %s\n", r.SourceSymbol, r.Relation, r.TargetSymbol, link))

		protoEdges = append(protoEdges, &pb.CodeGraphEdge{
			SourceSymbol: r.SourceSymbol,
			TargetSymbol: r.TargetSymbol,
			Relation:     r.Relation,
			FilePath:     r.FilePath,
			Line:         int32(r.Line),
		})
	}

	step.Action = &pb.StepUpdate_CodeGraph{
		CodeGraph: &pb.ActionCodeGraph{
			Operation:    "references",
			Query:        symbolID,
			Branch:       branch,
			Edges:        protoEdges,
			Summary:      fmt.Sprintf("Found %d references", len(refs)),
			TotalMatches: int32(len(refs)),
		},
	}
	step.Text = md.String()
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	return nil
}

// executeCodeGraphCallHierarchy handles the codegraph_call_hierarchy tool.
func (e *Engine) executeCodeGraphCallHierarchy(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	store, wsPath, err := e.getCodeGraphStore(tc)
	if err != nil {
		return fmt.Errorf("codegraph_call_hierarchy: %w", err)
	}

	symbolID, _ := tc.Args["symbol_id"].(string)
	direction, _ := tc.Args["direction"].(string)
	branch, _ := tc.Args["branch"].(string)
	depth := 3
	if depthFloat, ok := tc.Args["depth"].(float64); ok && depthFloat > 0 {
		depth = int(depthFloat)
	}

	if symbolID == "" {
		return fmt.Errorf("codegraph_call_hierarchy: symbol_id is required")
	}

	hierarchy := store.GetCallHierarchy(branch, symbolID, direction, depth)

	var protoNodes []*pb.CodeGraphNode
	var protoEdges []*pb.CodeGraphEdge
	var md strings.Builder
	md.WriteString(fmt.Sprintf("### Call Hierarchy (%s) for `%s`\n\n", hierarchy.Direction, symbolID))

	if len(hierarchy.Calls) == 0 {
		md.WriteString("No call connections found.\n")
	} else {
		for _, call := range hierarchy.Calls {
			indent := strings.Repeat("  ", call.Depth)
			absPath := filepath.Join(wsPath, call.FilePath)
			link := fmt.Sprintf("[%s:%d](file://%s#L%d)", call.FilePath, call.Line, absPath, call.Line)
			md.WriteString(fmt.Sprintf("%s- (depth %d) **%s** calls `%s` in %s\n", indent, call.Depth, call.CallerNode.SymbolID, call.CalleeSymbol, link))

			protoEdges = append(protoEdges, &pb.CodeGraphEdge{
				SourceSymbol: call.CallerNode.SymbolID,
				TargetSymbol: call.CalleeSymbol,
				Relation:     "calls",
				FilePath:     call.FilePath,
				Line:         int32(call.Line),
			})
		}
	}

	step.Action = &pb.StepUpdate_CodeGraph{
		CodeGraph: &pb.ActionCodeGraph{
			Operation:    "callers",
			Query:        symbolID,
			Branch:       branch,
			Depth:        int32(depth),
			Nodes:        protoNodes,
			Edges:        protoEdges,
			Summary:      fmt.Sprintf("Found %d calls in hierarchy", len(hierarchy.Calls)),
			TotalMatches: int32(len(hierarchy.Calls)),
		},
	}
	step.Text = md.String()
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	return nil
}

// executeCodeGraphGetImpact handles the codegraph_get_impact tool.
func (e *Engine) executeCodeGraphGetImpact(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	store, wsPath, err := e.getCodeGraphStore(tc)
	if err != nil {
		return fmt.Errorf("codegraph_get_impact: %w", err)
	}

	target, _ := tc.Args["target"].(string)
	branch, _ := tc.Args["branch"].(string)
	depth := 5
	if depthFloat, ok := tc.Args["depth"].(float64); ok && depthFloat > 0 {
		depth = int(depthFloat)
	}

	if target == "" {
		return fmt.Errorf("codegraph_get_impact: target is required")
	}

	impact := store.GetImpactRadius(branch, target, depth)

	var protoNodes []*pb.CodeGraphNode
	var md strings.Builder
	md.WriteString(fmt.Sprintf("### Impact Radius Analysis for `%s`\n\n", target))
	md.WriteString(fmt.Sprintf("- **Affected Files (%d):**\n", len(impact.AffectedFiles)))
	for _, f := range impact.AffectedFiles {
		absPath := filepath.Join(wsPath, f)
		md.WriteString(fmt.Sprintf("  - [%s](file://%s)\n", f, absPath))
	}

	md.WriteString(fmt.Sprintf("\n- **Downstream Callers (%d):**\n", len(impact.DownstreamCallers)))
	for _, c := range impact.DownstreamCallers {
		absPath := filepath.Join(wsPath, c.FilePath)
		link := fmt.Sprintf("[%s:%d](file://%s#L%d)", c.FilePath, c.StartLine, absPath, c.StartLine)
		md.WriteString(fmt.Sprintf("  - `%s` (`%s`) in %s\n", c.SymbolID, c.Kind, link))

		protoNodes = append(protoNodes, &pb.CodeGraphNode{
			SymbolId:  c.SymbolID,
			Kind:      c.Kind,
			Name:      c.Name,
			FilePath:  c.FilePath,
			StartLine: int32(c.StartLine),
			EndLine:   int32(c.EndLine),
		})
	}

	if len(impact.Implementations) > 0 {
		md.WriteString(fmt.Sprintf("\n- **Implementations / Subtypes (%d):**\n", len(impact.Implementations)))
		for _, im := range impact.Implementations {
			absPath := filepath.Join(wsPath, im.FilePath)
			link := fmt.Sprintf("[%s:%d](file://%s#L%d)", im.FilePath, im.StartLine, absPath, im.StartLine)
			md.WriteString(fmt.Sprintf("  - `%s` in %s\n", im.SymbolID, link))
		}
	}

	step.Action = &pb.StepUpdate_CodeGraph{
		CodeGraph: &pb.ActionCodeGraph{
			Operation:    "impact",
			Query:        target,
			Branch:       branch,
			Depth:        int32(depth),
			Nodes:        protoNodes,
			Summary:      fmt.Sprintf("Impact radius: %d callers, %d affected files", len(impact.DownstreamCallers), len(impact.AffectedFiles)),
			TotalMatches: int32(len(impact.DownstreamCallers)),
		},
	}
	step.Text = md.String()
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	return nil
}

// executeCodeGraphDiffBranches handles the codegraph_diff_branches tool.
func (e *Engine) executeCodeGraphDiffBranches(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	store, _, err := e.getCodeGraphStore(tc)
	if err != nil {
		return fmt.Errorf("codegraph_diff_branches: %w", err)
	}

	baseBranch, _ := tc.Args["base_branch"].(string)
	targetBranch, _ := tc.Args["target_branch"].(string)

	if baseBranch == "" || targetBranch == "" {
		return fmt.Errorf("codegraph_diff_branches: base_branch and target_branch are required")
	}

	diff := store.DiffBranches(baseBranch, targetBranch)

	var protoNodes []*pb.CodeGraphNode
	var protoEdges []*pb.CodeGraphEdge
	var md strings.Builder
	md.WriteString(fmt.Sprintf("### Code Graph Diff: `%s` ◄── `%s`\n\n", baseBranch, targetBranch))

	md.WriteString(fmt.Sprintf("- **Added Symbols (%d):**\n", len(diff.AddedNodes)))
	for _, n := range diff.AddedNodes {
		md.WriteString(fmt.Sprintf("  - `+` `%s` (`%s`) in `%s`\n", n.Name, n.Kind, n.FilePath))
		protoNodes = append(protoNodes, &pb.CodeGraphNode{
			SymbolId: n.SymbolID,
			Kind:     n.Kind,
			Name:     n.Name,
			FilePath: n.FilePath,
		})
	}

	md.WriteString(fmt.Sprintf("\n- **Removed Symbols (%d):**\n", len(diff.RemovedNodes)))
	for _, n := range diff.RemovedNodes {
		md.WriteString(fmt.Sprintf("  - `-` `%s` (`%s`) in `%s`\n", n.Name, n.Kind, n.FilePath))
	}

	md.WriteString(fmt.Sprintf("\n- **New Dependencies/Calls (%d):**\n", len(diff.AddedEdges)))
	for _, ed := range diff.AddedEdges {
		md.WriteString(fmt.Sprintf("  - `+` `%s` %s `%s`\n", ed.SourceSymbol, ed.Relation, ed.TargetSymbol))
		protoEdges = append(protoEdges, &pb.CodeGraphEdge{
			SourceSymbol: ed.SourceSymbol,
			TargetSymbol: ed.TargetSymbol,
			Relation:     ed.Relation,
			FilePath:     ed.FilePath,
			Line:         int32(ed.Line),
		})
	}

	step.Action = &pb.StepUpdate_CodeGraph{
		CodeGraph: &pb.ActionCodeGraph{
			Operation:    "diff",
			Branch:       baseBranch,
			TargetBranch: targetBranch,
			Nodes:        protoNodes,
			Edges:        protoEdges,
			Summary:      fmt.Sprintf("Diff: %d added symbols, %d removed symbols, %d added edges", len(diff.AddedNodes), len(diff.RemovedNodes), len(diff.AddedEdges)),
			TotalMatches: int32(len(diff.AddedNodes)),
		},
	}
	step.Text = md.String()
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	return nil
}
