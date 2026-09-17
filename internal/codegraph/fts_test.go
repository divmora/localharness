package codegraph

import (
	"testing"
)

func TestTokenizeText_CamelCaseAndSnakeCase(t *testing.T) {
	tokens := TokenizeText("ValidateTokenAndCheckUser_id")
	expected := map[string]bool{
		"validatetokenandcheckuser": true,
		"validate":                  true,
		"token":                     true,
		"and":                       true,
		"check":                     true,
		"user":                      true,
		"id":                        true,
	}

	for _, tok := range tokens {
		if !expected[tok] {
			t.Errorf("unexpected token %q", tok)
		}
	}
}

func TestFTSIndex_NaturalLanguageDocstringSearch(t *testing.T) {
	fts := NewFTSIndex()

	fts.IndexNode(Node{
		SymbolID:  "auth/jwt.go:ValidateToken",
		Kind:      "function",
		Name:      "ValidateToken",
		FilePath:  "auth/jwt.go",
		Signature: "func ValidateToken(raw string) (*Claims, error)",
		Docstring: "ValidateToken verifies cryptographic signatures and checks token expiration dates.",
	})

	fts.IndexNode(Node{
		SymbolID:  "storage/db.go:QueryUsers",
		Kind:      "function",
		Name:      "QueryUsers",
		FilePath:  "storage/db.go",
		Signature: "func QueryUsers(ctx context.Context) ([]User, error)",
		Docstring: "QueryUsers executes a SQL select query against the PostgreSQL database.",
	})

	fts.IndexNode(Node{
		SymbolID:  "engine/knowledge.go:DeleteArtifact",
		Kind:      "method",
		Name:      "DeleteArtifact",
		FilePath:  "engine/knowledge.go",
		Signature: "func (k *KnowledgeStore) DeleteArtifact(kiName string) error",
		Docstring: "DeleteArtifact removes an individual artifact file from disk and updates KI metadata.",
	})

	// Query 1: Exact function name
	res1 := fts.Search("ValidateToken", "", 10)
	if len(res1) == 0 || res1[0].Node.Name != "ValidateToken" {
		t.Fatalf("expected ValidateToken top match, got %v", res1)
	}

	// Query 2: Natural language docstring query "cryptographic signatures expiration"
	res2 := fts.Search("cryptographic signatures expiration", "", 10)
	if len(res2) == 0 || res2[0].Node.Name != "ValidateToken" {
		t.Fatalf("expected ValidateToken match on docstring terms, got %v", res2)
	}

	// Query 3: Multi-word query matching DeleteArtifact
	res3 := fts.Search("removes artifact metadata", "", 10)
	if len(res3) == 0 || res3[0].Node.Name != "DeleteArtifact" {
		t.Fatalf("expected DeleteArtifact match on docstring terms, got %v", res3)
	}
}

func TestFTSIndex_RemoveNode_AndIncrementalUpdate(t *testing.T) {
	fts := NewFTSIndex()

	nodeA := Node{
		SymbolID:  "auth/token.go:GenerateToken",
		Kind:      "function",
		Name:      "GenerateToken",
		FilePath:  "auth/token.go",
		Signature: "func GenerateToken(userID string) string",
		Docstring: "GenerateToken creates a cryptographic JWT signature with claims.",
	}
	nodeB := Node{
		SymbolID:  "auth/token.go:VerifyToken",
		Kind:      "function",
		Name:      "VerifyToken",
		FilePath:  "auth/token.go",
		Signature: "func VerifyToken(token string) bool",
		Docstring: "VerifyToken inspects token signature against public key.",
	}

	fts.IndexNode(nodeA)
	fts.IndexNode(nodeB)

	if fts.Len() != 2 {
		t.Fatalf("expected 2 documents, got %d", fts.Len())
	}
	if !fts.HasNode("auth/token.go:GenerateToken") {
		t.Fatal("expected HasNode to be true for GenerateToken")
	}

	// Verify initial search finds GenerateToken
	res := fts.Search("cryptographic", "", 10)
	if len(res) == 0 || res[0].Node.Name != "GenerateToken" {
		t.Fatalf("expected search for 'cryptographic' to find GenerateToken, got %v", res)
	}

	// Remove GenerateToken
	fts.RemoveNode("auth/token.go:GenerateToken")
	if fts.Len() != 1 {
		t.Fatalf("expected 1 document after removal, got %d", fts.Len())
	}
	if fts.HasNode("auth/token.go:GenerateToken") {
		t.Fatal("expected HasNode to be false after removal")
	}

	// Search for removed node's unique term must return 0 results
	resAfter := fts.Search("cryptographic", "", 10)
	if len(resAfter) != 0 {
		t.Fatalf("expected 0 results for removed term 'cryptographic', got %v", resAfter)
	}

	// Search for remaining node should still succeed
	resRemaining := fts.Search("VerifyToken", "", 10)
	if len(resRemaining) == 0 || resRemaining[0].Node.Name != "VerifyToken" {
		t.Fatalf("expected search to still find VerifyToken, got %v", resRemaining)
	}

	// Test removing non-existent symbol is a safe no-op
	fts.RemoveNode("nonexistent/symbol:DoSomething")
	if fts.Len() != 1 {
		t.Fatalf("expected docCount to remain 1 after removing nonexistent symbol, got %d", fts.Len())
	}

	// Test idempotent re-indexing: replace VerifyToken with updated docstring
	updatedNodeB := Node{
		SymbolID:  "auth/token.go:VerifyToken",
		Kind:      "function",
		Name:      "VerifyToken",
		FilePath:  "auth/token.go",
		Signature: "func VerifyToken(token string) bool",
		Docstring: "VerifyToken validates quantum asymmetric signatures.",
	}
	fts.IndexNode(updatedNodeB)
	if fts.Len() != 1 {
		t.Fatalf("expected docCount to stay 1 after updating same symbol, got %d", fts.Len())
	}

	// New term 'quantum' should now match
	resQuantum := fts.Search("quantum", "", 10)
	if len(resQuantum) == 0 || resQuantum[0].Node.Name != "VerifyToken" {
		t.Fatalf("expected search for 'quantum' to find updated VerifyToken, got %v", resQuantum)
	}
}

func TestMultiLanguageParsing_Python(t *testing.T) {
	pyCode := `
class BaseService:
    """Base abstract service."""
    pass

class AuthService(BaseService):
    """Handles authentication and session token verification."""
    def __init__(self, db_client):
        self.db = db_client

    async def verify_credentials(self, user: str, token: str) -> bool:
        """Verifies HMAC signature on user tokens."""
        return self.validate(user, token)
`

	nodes, edges, err := ParseSourceFile("auth/service.py", []byte(pyCode))
	if err != nil {
		t.Fatalf("parse python: %v", err)
	}

	if len(nodes) < 3 {
		t.Fatalf("expected at least 3 nodes (2 classes, methods), got %d", len(nodes))
	}

	var foundAuthService, foundVerify bool
	for _, n := range nodes {
		if n.Name == "AuthService" && n.Kind == "class" {
			foundAuthService = true
			if n.Docstring == "" {
				t.Errorf("expected docstring for AuthService")
			}
		}
		if n.Name == "verify_credentials" && n.Kind == "method" {
			foundVerify = true
			if n.Docstring == "" {
				t.Errorf("expected docstring for verify_credentials")
			}
		}
	}

	if !foundAuthService || !foundVerify {
		t.Errorf("did not find expected python symbols")
	}

	var foundInherit bool
	for _, e := range edges {
		if e.Relation == "inherits" && e.TargetSymbol == "BaseService" {
			foundInherit = true
		}
	}
	if !foundInherit {
		t.Errorf("expected inheritance edge to BaseService")
	}
}

func TestMultiLanguageParsing_TypeScript(t *testing.T) {
	tsCode := `
import { Database } from './db';

export interface UserSession {
    userId: string;
    expiresAt: number;
}

/**
 * Handles active user authentication.
 */
export class SessionManager implements UserSession {
    userId: string;
    expiresAt: number;

    /**
     * Authenticates an incoming token.
     */
    export const authenticateUser = async (token: string): Promise<boolean> => {
        return checkToken(token);
    };
}
`

	nodes, edges, err := ParseSourceFile("session/auth.ts", []byte(tsCode))
	if err != nil {
		t.Fatalf("parse ts: %v", err)
	}

	if len(nodes) < 3 {
		t.Fatalf("expected interface, class, and function nodes, got %d", len(nodes))
	}

	var foundIface, foundClass, foundFunc bool
	for _, n := range nodes {
		if n.Name == "UserSession" && n.Kind == "interface" {
			foundIface = true
		}
		if n.Name == "SessionManager" && n.Kind == "class" {
			foundClass = true
			if n.Docstring == "" {
				t.Errorf("expected JSDoc for SessionManager")
			}
		}
		if n.Name == "authenticateUser" && n.Kind == "function" {
			foundFunc = true
			if n.Docstring == "" {
				t.Errorf("expected JSDoc for authenticateUser")
			}
		}
	}

	if !foundIface || !foundClass || !foundFunc {
		t.Errorf("did not find expected TS symbols")
	}

	var foundImplements bool
	for _, e := range edges {
		if e.Relation == "implements" && e.TargetSymbol == "UserSession" {
			foundImplements = true
		}
	}
	if !foundImplements {
		t.Errorf("expected implements edge to UserSession")
	}
}

func TestMultiLanguageParsing_Rust(t *testing.T) {
	rsCode := `
use std::sync::Arc;

pub trait TokenVerifier {
    fn verify(&self, token: &str) -> bool;
}

pub struct JwtService {
    secret: String,
}

impl TokenVerifier for JwtService {
    pub async fn verify(&self, token: &str) -> bool {
        true
    }
}
`

	nodes, edges, err := ParseSourceFile("auth/jwt.rs", []byte(rsCode))
	if err != nil {
		t.Fatalf("parse rust: %v", err)
	}

	var foundTrait, foundStruct, foundImpl bool
	for _, n := range nodes {
		if n.Name == "TokenVerifier" && n.Kind == "trait" {
			foundTrait = true
		}
		if n.Name == "JwtService" && n.Kind == "struct" {
			foundStruct = true
		}
	}

	for _, e := range edges {
		if e.Relation == "implements" && e.TargetSymbol == "TokenVerifier" {
			foundImpl = true
		}
	}

	if !foundTrait || !foundStruct || !foundImpl {
		t.Errorf("failed rust symbol or relation extraction")
	}
}
