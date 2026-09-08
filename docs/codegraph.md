# Repository Code Graph (`codegraph`)

LocalHarness includes a built-in semantic AST **Code Graph** system inspired by GitLab Orbit and Sourcegraph SCIP. It indexes code symbols, definitions, type hierarchies, and cross-file call relationships into a per-project DuckDB-compatible database.

## Key Highlights

- **Embedded DuckDB Storage**: Saved at `~/.divmora/localharness/knowledge/<project-uuid>/codegraph.duckdb`.
- **Content-Addressed (Git-style) De-duplication**: Code symbols and edges are indexed by file content SHA-256 (`blob_hash`). Unchanged files across branches take 0 ms to index and 0 extra bytes on disk.
- **Branch-Aware Manifests**: Tracks git branches independently via `file_manifest`, allowing instant branch switching and cross-branch graph diffs (`DiffBranches`).
- **Live Incremental Freshness**: Automatically re-indexes modified files on `write_to_file` and `replace_file_content` without full rescans.
- **Multi-Language Support**: Built-in parsers for Go (`go/ast`), Python, TypeScript/JavaScript, Rust, and Protobuf.

---

## Schema Architecture

```sql
-- Track branches
CREATE TABLE branches (
    branch_name VARCHAR PRIMARY KEY,
    head_commit VARCHAR,
    updated_at TIMESTAMP
);

-- Branch file manifest -> content blob hash
CREATE TABLE file_manifest (
    branch_name VARCHAR,
    file_path VARCHAR,
    blob_hash VARCHAR,
    PRIMARY KEY (branch_name, file_path)
);

-- Content-addressed AST nodes
CREATE TABLE nodes (
    blob_hash VARCHAR,
    symbol_id VARCHAR,
    kind VARCHAR,            -- "function", "method", "struct", "interface", "class", "type"
    name VARCHAR,
    file_path VARCHAR,
    start_line INTEGER,
    end_line INTEGER,
    signature VARCHAR,
    docstring VARCHAR,
    exported BOOLEAN,
    PRIMARY KEY (blob_hash, symbol_id)
);

-- Content-addressed relationship edges
CREATE TABLE edges (
    blob_hash VARCHAR,
    source_symbol VARCHAR,
    target_symbol VARCHAR,
    relation VARCHAR,        -- "calls", "imports", "implements", "references", "inherits"
    file_path VARCHAR,
    line INTEGER,
    PRIMARY KEY (blob_hash, source_symbol, target_symbol, relation)
);
```

---

## Agent Built-in Tools

When `code_graph` is enabled (default `true` in `BuiltinToolsConfig`), the agent has access to structural reasoning tools:

| Tool | Purpose | Parameters |
|:---|:---|:---|
| `codegraph_search` | Search symbols, types, and functions | `query`, `kind` (optional), `branch` (optional), `limit` |
| `codegraph_find_references` | Locate call sites and usages | `symbol_id`, `branch` (optional) |
| `codegraph_call_hierarchy` | Multi-hop call tree (incoming / outgoing) | `symbol_id`, `direction` ("incoming"\|"outgoing"), `depth`, `branch` |
| `codegraph_get_impact` | Compute blast radius before refactoring | `target` (symbol or file path), `depth`, `branch` |
| `codegraph_diff_branches` | Compare graph changes between branches | `base_branch`, `target_branch` |

---

## Full-Text Search (FTS) & Natural Language Symbol Search

`code-graph` features a built-in BM25 inverted index that tokenizes CamelCase, snake_case, docstrings, and signatures:
- **Weighted Relevance**: Symbol names (3x), signatures (2x), and docstrings (2x) are ranked with BM25 term-frequency and inverse-document-frequency scoring.
- **Natural Language Queries**: Querying phrases like `"verify cryptographic signatures"` or `"delete artifact metadata"` surfaces the exact function or method even if the name isn't an exact substring match.

---

## Supported Languages & AST Features

| Language | Extracted Entities | Relationships |
|:---|:---|:---|
| **Go** | Packages, structs, interfaces, functions, methods, doc comments | `imports`, `calls`, `defines` |
| **Python** | Classes, async/sync functions, methods, decorators, triple-quote docstrings | `imports`, `inherits`, `calls` |
| **TypeScript / JS** | Interfaces, types, enums, classes, arrow functions, JSDoc | `imports`, `implements`, `inherits`, `calls` |
| **Rust** | Structs, enums, traits, `impl` blocks, async fns, `///` docs | `imports`, `implements`, `calls` |
| **Protobuf** | Messages, enums, services, RPCs, options | `imports`, `references` |

---

## CLI Usage (`lhctl codegraph`)

You can inspect, search, and query the code graph directly from your terminal:

```bash
# 1. Index the workspace (full or incremental)
lhctl codegraph index

# 2. Check graph status and DuckDB database path
lhctl codegraph status

# 3. Full-Text Search for symbols by natural language or signature
lhctl codegraph search "BM25 relevance scoring"

# 4. Filter symbols by kind (struct, function, interface, class)
lhctl codegraph search "KnowledgeStore" --kind=struct

# 5. Inspect incoming call hierarchy
lhctl codegraph callers "NewKnowledgeStore" --depth=4

# 6. Compute blast radius / downstream affected files before refactoring
lhctl codegraph impact "internal/engine/knowledge.go"

# 7. Diff code graphs between two branches
lhctl codegraph diff main feat/new-storage
```
