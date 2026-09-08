package codegraph

import (
	"context"
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
