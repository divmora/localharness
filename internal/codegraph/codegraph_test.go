package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestGoParser(t *testing.T) {
	goCode := `package auth

import (
	"fmt"
	"time"
)

type TokenValidator interface {
	Validate(token string) bool
}

type User struct {
	ID   string
	Name string
}

func (u *User) GetDisplayName() string {
	return u.Name
}

func AuthenticateUser(token string) (*User, error) {
	fmt.Println("Authenticating...")
	user := &User{ID: "123", Name: "Alice"}
	_ = user.GetDisplayName()
	return user, nil
}
`
	nodes, edges, err := ParseSourceFile("auth/auth.go", []byte(goCode))
	if err != nil {
		t.Fatalf("ParseSourceFile failed: %v", err)
	}

	if len(nodes) == 0 {
		t.Fatalf("expected nodes, got 0")
	}

	foundStruct := false
	foundInterface := false
	foundMethod := false
	foundFunc := false

	for _, n := range nodes {
		switch n.Kind {
		case "struct":
			if n.Name == "User" {
				foundStruct = true
			}
		case "interface":
			if n.Name == "TokenValidator" {
				foundInterface = true
			}
		case "method":
			if n.Name == "GetDisplayName" {
				foundMethod = true
			}
		case "function":
			if n.Name == "AuthenticateUser" {
				foundFunc = true
			}
		}
	}

	if !foundStruct {
		t.Errorf("expected to find struct 'User'")
	}
	if !foundInterface {
		t.Errorf("expected to find interface 'TokenValidator'")
	}
	if !foundMethod {
		t.Errorf("expected to find method 'GetDisplayName'")
	}
	if !foundFunc {
		t.Errorf("expected to find function 'AuthenticateUser'")
	}

	foundCall := false
	for _, e := range edges {
		if e.Relation == "calls" && e.TargetSymbol == "fmt.Println" {
			foundCall = true
		}
	}
	if !foundCall {
		t.Errorf("expected to find call edge to fmt.Println")
	}
}

func TestMultiLanguageParser(t *testing.T) {
	pyCode := `class AuthService(BaseService):
    def validate_token(self, token):
        pass
`
	nodes, edges, err := ParseSourceFile("auth.py", []byte(pyCode))
	if err != nil {
		t.Fatalf("Python parse failed: %v", err)
	}
	if len(nodes) < 2 {
		t.Errorf("expected at least 2 nodes in python file, got %d", len(nodes))
	}
	if len(edges) < 1 {
		t.Errorf("expected inherits edge in python file")
	}

	tsCode := `export interface UserProfile {
    id: string;
}

export function getUser(): UserProfile {
    return { id: "1" };
}
`
	tsNodes, _, err := ParseSourceFile("user.ts", []byte(tsCode))
	if err != nil {
		t.Fatalf("TS parse failed: %v", err)
	}
	if len(tsNodes) < 2 {
		t.Errorf("expected interface and function in TS, got %d", len(tsNodes))
	}
}

func TestStore_ContentAddressedAndBranches(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "codegraph.duckdb")
	store := NewStore(dbPath)

	hashA := "hash-a1b2c3"
	nodesA := []Node{
		{SymbolID: "pkg/auth:Validate", Name: "Validate", Kind: "function", FilePath: "auth.go"},
	}
	edgesA := []Edge{
		{SourceSymbol: "pkg/auth:Validate", TargetSymbol: "crypto:Verify", Relation: "calls", FilePath: "auth.go"},
	}

	// Add to main branch
	store.AddFile("main", "auth.go", hashA, nodesA, edgesA)

	// Switch branch to feat/new-auth and add modified file
	hashB := "hash-x9y8z7"
	nodesB := []Node{
		{SymbolID: "pkg/auth:ValidateV2", Name: "ValidateV2", Kind: "function", FilePath: "auth.go"},
	}
	edgesB := []Edge{
		{SourceSymbol: "pkg/auth:ValidateV2", TargetSymbol: "crypto:VerifyV2", Relation: "calls", FilePath: "auth.go"},
	}
	store.AddFile("feat/new-auth", "auth.go", hashB, nodesB, edgesB)

	// Verify main
	mainNodes := store.GetBranchNodes("main")
	if len(mainNodes) != 1 || mainNodes[0].Name != "Validate" {
		t.Fatalf("expected Validate on main, got %+v", mainNodes)
	}

	// Verify feat/new-auth
	featNodes := store.GetBranchNodes("feat/new-auth")
	if len(featNodes) != 1 || featNodes[0].Name != "ValidateV2" {
		t.Fatalf("expected ValidateV2 on feat/new-auth, got %+v", featNodes)
	}

	// Test diff
	diff := store.DiffBranches("main", "feat/new-auth")
	if len(diff.AddedNodes) != 1 || diff.AddedNodes[0].Name != "ValidateV2" {
		t.Errorf("diff added nodes mismatch: %+v", diff.AddedNodes)
	}
	if len(diff.RemovedNodes) != 1 || diff.RemovedNodes[0].Name != "Validate" {
		t.Errorf("diff removed nodes mismatch: %+v", diff.RemovedNodes)
	}

	// Save and reload
	if err := store.Save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	store2 := NewStore(dbPath)
	if err := store2.Load(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	reloadedMain := store2.GetBranchNodes("main")
	if len(reloadedMain) != 1 || reloadedMain[0].Name != "Validate" {
		t.Fatalf("reloaded store mismatch: %+v", reloadedMain)
	}
}

func TestStore_CallHierarchyAndImpact(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "codegraph.duckdb")
	store := NewStore(dbPath)

	hash := "blob-123"
	nodes := []Node{
		{SymbolID: "handler:Login", Name: "Login", Kind: "function", FilePath: "handler.go"},
		{SymbolID: "service:Auth", Name: "Auth", Kind: "function", FilePath: "service.go"},
		{SymbolID: "db:GetUser", Name: "GetUser", Kind: "function", FilePath: "db.go"},
	}
	edges := []Edge{
		{SourceSymbol: "handler:Login", TargetSymbol: "service:Auth", Relation: "calls", FilePath: "handler.go", Line: 15},
		{SourceSymbol: "service:Auth", TargetSymbol: "db:GetUser", Relation: "calls", FilePath: "service.go", Line: 25},
	}

	store.AddFile("main", "all.go", hash, nodes, edges)

	// Call hierarchy incoming to db:GetUser
	hierarchy := store.GetCallHierarchy("main", "db:GetUser", "incoming", 5)
	if len(hierarchy.Calls) < 1 {
		t.Fatalf("expected incoming calls to db:GetUser, got %d", len(hierarchy.Calls))
	}

	// Impact of changing db.go or db:GetUser
	impact := store.GetImpactRadius("main", "db:GetUser", 5)
	if len(impact.DownstreamCallers) < 2 {
		t.Errorf("expected at least 2 downstream callers (Auth, Login), got %d", len(impact.DownstreamCallers))
	}
	if len(impact.AffectedFiles) < 2 {
		t.Errorf("expected affected files (handler.go, service.go), got %d", len(impact.AffectedFiles))
	}
}

func TestIndexer_Workspace(t *testing.T) {
	wsDir := t.TempDir()
	knowledgeDir := t.TempDir()

	// Create sample Go file
	srcPath := filepath.Join(wsDir, "main.go")
	srcCode := `package main

func EntryPoint() {
	println("hello")
}
`
	if err := os.WriteFile(srcPath, []byte(srcCode), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	mgr := NewManager(knowledgeDir)
	mgr.RegisterWorkspace(wsDir, "proj-test-123")

	stats, err := mgr.IndexWorkspace(context.Background(), "proj-test-123", wsDir, "main")
	if err != nil {
		t.Fatalf("index workspace failed: %v", err)
	}

	if stats.TotalFiles != 1 || stats.ParsedFiles != 1 {
		t.Errorf("unexpected index stats: %+v", stats)
	}

	store, err := mgr.GetStore("proj-test-123")
	if err != nil {
		t.Fatalf("get store failed: %v", err)
	}

	results := store.SearchSymbols("main", "EntryPoint", "function", 10)
	if len(results) != 1 || results[0].Name != "EntryPoint" {
		t.Errorf("expected to find EntryPoint symbol, got %+v", results)
	}

	// Incremental update test
	updatedCode := `package main

func EntryPoint() {
	println("hello updated")
}

func Secondary() {}
`
	if err := os.WriteFile(srcPath, []byte(updatedCode), 0644); err != nil {
		t.Fatalf("write updated file failed: %v", err)
	}

	if err := mgr.UpdateFile(context.Background(), wsDir, "main.go"); err != nil {
		t.Fatalf("update file failed: %v", err)
	}

	results2 := store.SearchSymbols("main", "Secondary", "function", 10)
	if len(results2) != 1 || results2[0].Name != "Secondary" {
		t.Errorf("expected to find newly added Secondary symbol, got %+v", results2)
	}
}

func TestStore_IncrementalFTSAndPrune(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "codegraph.duckdb")
	store := NewStore(dbPath)

	hash1 := "blob-file1-v1"
	nodes1 := []Node{
		{SymbolID: "pkg/auth:Login", Name: "Login", Kind: "function", FilePath: "auth.go", Docstring: "Login handles credential verification."},
		{SymbolID: "pkg/auth:Logout", Name: "Logout", Kind: "function", FilePath: "auth.go", Docstring: "Logout destroys the user session."},
	}
	store.AddFile("main", "auth.go", hash1, nodes1, nil)

	// Search initially populates FTS cache
	res1 := store.SearchSymbols("main", "credential", "", 10)
	if len(res1) != 1 || res1[0].Name != "Login" {
		t.Fatalf("expected Login for 'credential', got %+v", res1)
	}

	// Update auth.go: replace Logout with RefreshToken and modify Login docstring
	hash1v2 := "blob-file1-v2"
	nodes1v2 := []Node{
		{SymbolID: "pkg/auth:Login", Name: "Login", Kind: "function", FilePath: "auth.go", Docstring: "Login performs multi-factor biometric verification."},
		{SymbolID: "pkg/auth:RefreshToken", Name: "RefreshToken", Kind: "function", FilePath: "auth.go", Docstring: "RefreshToken extends valid session tokens."},
	}
	store.AddFile("main", "auth.go", hash1v2, nodes1v2, nil)

	// Removed symbol Logout shouldn't be found
	resLogout := store.SearchSymbols("main", "Logout", "", 10)
	if len(resLogout) != 0 {
		t.Fatalf("expected 0 results for removed symbol Logout, got %+v", resLogout)
	}

	// Newly added symbol RefreshToken should be found
	resRefresh := store.SearchSymbols("main", "RefreshToken", "", 10)
	if len(resRefresh) != 1 || resRefresh[0].Name != "RefreshToken" {
		t.Fatalf("expected RefreshToken, got %+v", resRefresh)
	}

	// Updated docstring for Login: 'biometric' matches, old 'credential' does not
	resBio := store.SearchSymbols("main", "biometric", "", 10)
	if len(resBio) != 1 || resBio[0].Name != "Login" {
		t.Fatalf("expected Login for 'biometric', got %+v", resBio)
	}
	resCred := store.SearchSymbols("main", "credential", "", 10)
	if len(resCred) != 0 {
		t.Fatalf("expected 0 results for old term 'credential', got %+v", resCred)
	}

	// Remove file entirely via RemoveFile
	store.RemoveFile("main", "auth.go")
	resEmpty := store.SearchSymbols("main", "Login", "", 10)
	if len(resEmpty) != 0 {
		t.Fatalf("expected 0 results after removing file, got %+v", resEmpty)
	}

	// Add two files and test PruneDeletedFiles
	store.AddFile("main", "fileA.go", "hashA", []Node{
		{SymbolID: "pkg:Alpha", Name: "Alpha", Kind: "function", FilePath: "fileA.go", Docstring: "Alpha documentation"},
	}, nil)
	store.AddFile("main", "fileB.go", "hashB", []Node{
		{SymbolID: "pkg:Beta", Name: "Beta", Kind: "function", FilePath: "fileB.go", Docstring: "Beta documentation"},
	}, nil)

	if len(store.SearchSymbols("main", "Alpha", "", 10)) != 1 {
		t.Fatal("expected to find Alpha")
	}
	if len(store.SearchSymbols("main", "Beta", "", 10)) != 1 {
		t.Fatal("expected to find Beta")
	}

	// Prune with active files only containing fileA.go
	store.PruneDeletedFiles("main", map[string]bool{"fileA.go": true})

	if len(store.SearchSymbols("main", "Alpha", "", 10)) != 1 {
		t.Fatal("expected Alpha to still exist after prune")
	}
	if len(store.SearchSymbols("main", "Beta", "", 10)) != 0 {
		t.Fatal("expected Beta to be pruned")
	}
}

func TestStore_RelationshipLookups_ExactColonDot(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "codegraph.duckdb")
	store := NewStore(dbPath)

	nodes := []Node{
		{SymbolID: "pkg/auth:User.GetDisplayName", Name: "GetDisplayName", Kind: "method", FilePath: "user.go"},
		{SymbolID: "pkg/service:AuthHandler", Name: "AuthHandler", Kind: "function", FilePath: "handler.go"},
		{SymbolID: "pkg/api:Router", Name: "Router", Kind: "function", FilePath: "router.go"},
		{SymbolID: "pkg/db:SessionStore", Name: "SessionStore", Kind: "interface", FilePath: "store.go"},
		{SymbolID: "pkg/db:RedisStore", Name: "RedisStore", Kind: "struct", FilePath: "redis.go"},
	}
	edges := []Edge{
		{SourceSymbol: "pkg/service:AuthHandler", TargetSymbol: "pkg/auth:User.GetDisplayName", Relation: "calls", FilePath: "handler.go", Line: 42},
		{SourceSymbol: "pkg/api:Router", TargetSymbol: "pkg/service:AuthHandler", Relation: "calls", FilePath: "router.go", Line: 10},
		{SourceSymbol: "pkg/db:RedisStore", TargetSymbol: "pkg/db:SessionStore", Relation: "implements", FilePath: "redis.go", Line: 5},
	}
	store.AddFile("main", "all.go", "hash-rel", nodes, edges)

	// 1. FindReferences matching:
	// a. Exact symbol ID
	refsExact := store.FindReferences("main", "pkg/auth:User.GetDisplayName")
	if len(refsExact) != 1 || refsExact[0].SourceSymbol != "pkg/service:AuthHandler" {
		t.Fatalf("expected 1 reference for exact symbol, got %+v", refsExact)
	}

	// b. Colon suffix "User.GetDisplayName"
	refsColon := store.FindReferences("main", "User.GetDisplayName")
	if len(refsColon) != 1 || refsColon[0].SourceSymbol != "pkg/service:AuthHandler" {
		t.Fatalf("expected 1 reference for colon suffix, got %+v", refsColon)
	}

	// c. Dot suffix "GetDisplayName"
	refsDot := store.FindReferences("main", "GetDisplayName")
	if len(refsDot) != 1 || refsDot[0].SourceSymbol != "pkg/service:AuthHandler" {
		t.Fatalf("expected 1 reference for dot suffix, got %+v", refsDot)
	}

	// 2. CallHierarchy incoming & outgoing
	// Incoming calls to User.GetDisplayName
	incoming := store.GetCallHierarchy("main", "GetDisplayName", "incoming", 5)
	if len(incoming.Calls) < 2 {
		t.Fatalf("expected at least 2 incoming calls in chain (AuthHandler, Router), got %d", len(incoming.Calls))
	}

	// Outgoing calls from Router
	outgoing := store.GetCallHierarchy("main", "pkg/api:Router", "outgoing", 5)
	if len(outgoing.Calls) < 2 {
		t.Fatalf("expected at least 2 outgoing calls from Router, got %d", len(outgoing.Calls))
	}

	// 3. ImpactRadius for SessionStore implementation
	impact := store.GetImpactRadius("main", "SessionStore", 5)
	if len(impact.Implementations) != 1 || impact.Implementations[0].Name != "RedisStore" {
		t.Fatalf("expected RedisStore implementation, got %+v", impact.Implementations)
	}
	if len(impact.AffectedFiles) < 2 {
		t.Fatalf("expected affected files for SessionStore, got %+v", impact.AffectedFiles)
	}

	// 4. ImpactRadius for file path
	impactFile := store.GetImpactRadius("main", "user.go", 5)
	if len(impactFile.DownstreamCallers) < 2 {
		t.Fatalf("expected downstream callers for user.go change, got %+v", impactFile.DownstreamCallers)
	}
}

func BenchmarkSearchSymbolsFTS(b *testing.B) {
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "codegraph.duckdb")
	store := NewStore(dbPath)

	// Create 1000 nodes
	var nodes []Node
	for i := 0; i < 1000; i++ {
		nodes = append(nodes, Node{
			SymbolID:  fmt.Sprintf("pkg/mod%d:Function%d", i%10, i),
			Name:      fmt.Sprintf("Function%d", i),
			Kind:      "function",
			FilePath:  fmt.Sprintf("mod%d/file%d.go", i%10, i),
			Signature: fmt.Sprintf("func Function%d(ctx context.Context) error", i),
			Docstring: fmt.Sprintf("Function%d executes operation %d against persistent storage.", i, i),
		})
	}
	store.AddFile("main", "all.go", "hash-benchmark", nodes, nil)

	// Warm up FTS cache
	store.SearchSymbolsFTS("main", "Function500", "", 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := store.SearchSymbolsFTS("main", "Function500", "", 10)
		if len(res) == 0 {
			b.Fatal("expected match")
		}
	}
}

func BenchmarkGetCallHierarchy(b *testing.B) {
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "codegraph.duckdb")
	store := NewStore(dbPath)

	// Create a call chain of 200 nodes and 200 edges
	var nodes []Node
	var edges []Edge
	for i := 0; i < 200; i++ {
		sym := fmt.Sprintf("pkg/service:Step%d", i)
		nodes = append(nodes, Node{
			SymbolID: sym,
			Name:     fmt.Sprintf("Step%d", i),
			Kind:     "function",
			FilePath: "service.go",
		})
		if i > 0 {
			prev := fmt.Sprintf("pkg/service:Step%d", i-1)
			edges = append(edges, Edge{
				SourceSymbol: prev,
				TargetSymbol: sym,
				Relation:     "calls",
				FilePath:     "service.go",
				Line:         i * 10,
			})
		}
	}
	store.AddFile("main", "service.go", "hash-hierarchy", nodes, edges)

	// Warm up graph cache
	store.GetCallHierarchy("main", "Step199", "incoming", 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hierarchy := store.GetCallHierarchy("main", "Step199", "incoming", 5)
		if len(hierarchy.Calls) == 0 {
			b.Fatal("expected calls")
		}
	}
}
