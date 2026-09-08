package codegraph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// ComputeBlobHash calculates the SHA-256 hash of a file's content.
func ComputeBlobHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// ParseSourceFile extracts AST nodes and relationship edges from file content across multiple languages.
func ParseSourceFile(relPath string, content []byte) ([]Node, []Edge, error) {
	blobHash := ComputeBlobHash(content)
	ext := strings.ToLower(filepath.Ext(relPath))

	switch ext {
	case ".go":
		return parseGoFile(relPath, blobHash, content)
	case ".py":
		return parsePythonFile(relPath, blobHash, content)
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return parseJSOrTSFile(relPath, blobHash, content)
	case ".rs":
		return parseRustFile(relPath, blobHash, content)
	case ".proto":
		return parseProtoFile(relPath, blobHash, content)
	default:
		return nil, nil, nil
	}
}

// parseGoFile uses Go's standard ast/parser to extract symbols, types, functions, and call edges.
func parseGoFile(relPath, blobHash string, content []byte) ([]Node, []Edge, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, relPath, content, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse go file %s: %w", relPath, err)
	}

	var nodes []Node
	var edges []Edge

	pkgName := file.Name.Name
	pkgSymbolID := fmt.Sprintf("pkg/%s", pkgName)

	nodes = append(nodes, Node{
		SymbolID:  pkgSymbolID,
		BlobHash:  blobHash,
		Kind:      "package",
		Name:      pkgName,
		FilePath:  relPath,
		StartLine: fset.Position(file.Package).Line,
		EndLine:   fset.Position(file.Package).Line,
		Exported:  true,
	})

	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		line := fset.Position(imp.Pos()).Line
		edges = append(edges, Edge{
			BlobHash:     blobHash,
			SourceSymbol: pkgSymbolID,
			TargetSymbol: importPath,
			Relation:     "imports",
			FilePath:     relPath,
			Line:         line,
		})
	}

	getDoc := func(cg *ast.CommentGroup) string {
		if cg == nil {
			return ""
		}
		return strings.TrimSpace(cg.Text())
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					name := ts.Name.Name
					symbolID := fmt.Sprintf("%s:%s", relPath, name)
					startLine := fset.Position(ts.Pos()).Line
					endLine := fset.Position(ts.End()).Line
					exported := isExported(name)

					kind := "type"
					signature := fmt.Sprintf("type %s", name)

					switch typeNode := ts.Type.(type) {
					case *ast.StructType:
						kind = "struct"
						signature = fmt.Sprintf("type %s struct", name)
					case *ast.InterfaceType:
						kind = "interface"
						signature = fmt.Sprintf("type %s interface", name)
						if typeNode.Methods != nil {
							for _, method := range typeNode.Methods.List {
								for _, mName := range method.Names {
									methodSymbolID := fmt.Sprintf("%s.%s", symbolID, mName.Name)
									edges = append(edges, Edge{
										BlobHash:     blobHash,
										SourceSymbol: symbolID,
										TargetSymbol: methodSymbolID,
										Relation:     "defines",
										FilePath:     relPath,
										Line:         fset.Position(method.Pos()).Line,
									})
								}
							}
						}
					}

					nodes = append(nodes, Node{
						SymbolID:  symbolID,
						BlobHash:  blobHash,
						Kind:      kind,
						Name:      name,
						FilePath:  relPath,
						StartLine: startLine,
						EndLine:   endLine,
						Signature: signature,
						Docstring: getDoc(d.Doc),
						Exported:  exported,
					})
				}
			}

		case *ast.FuncDecl:
			funcName := d.Name.Name
			startLine := fset.Position(d.Pos()).Line
			endLine := fset.Position(d.End()).Line
			exported := isExported(funcName)
			kind := "function"
			symbolID := fmt.Sprintf("%s:%s", relPath, funcName)
			sig := formatGoFuncSig(d)

			if d.Recv != nil && len(d.Recv.List) > 0 {
				kind = "method"
				recvType := formatGoExpr(d.Recv.List[0].Type)
				cleanRecv := strings.TrimPrefix(recvType, "*")
				symbolID = fmt.Sprintf("%s:%s.%s", relPath, cleanRecv, funcName)
			}

			nodes = append(nodes, Node{
				SymbolID:  symbolID,
				BlobHash:  blobHash,
				Kind:      kind,
				Name:      funcName,
				FilePath:  relPath,
				StartLine: startLine,
				EndLine:   endLine,
				Signature: sig,
				Docstring: getDoc(d.Doc),
				Exported:  exported,
			})

			if d.Body != nil {
				ast.Inspect(d.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}

					callLine := fset.Position(call.Pos()).Line
					calleeName := formatGoExpr(call.Fun)
					if calleeName != "" {
						edges = append(edges, Edge{
							BlobHash:     blobHash,
							SourceSymbol: symbolID,
							TargetSymbol: calleeName,
							Relation:     "calls",
							FilePath:     relPath,
							Line:         callLine,
						})
					}
					return true
				})
			}
		}
	}

	return nodes, edges, nil
}

// Regex matchers for advanced non-Go language parsing
var (
	pyClassRegex  = regexp.MustCompile(`(?m)^[ \t]*class\s+([a-zA-Z0-9_]+)(?:\s*\((.*?)\))?\s*:`)
	pyFuncRegex   = regexp.MustCompile(`(?m)^([ \t]*)(?:async\s+)?def\s+([a-zA-Z0-9_]+)\s*\((.*?)\)(?:\s*->\s*(.*?))?\s*:`)
	pyImportRegex = regexp.MustCompile(`(?m)^[ \t]*(?:from\s+([a-zA-Z0-9_.]+)\s+import\s+([a-zA-Z0-9_., *()]+)|import\s+([a-zA-Z0-9_., ]+))`)
	pyCallRegex   = regexp.MustCompile(`\b([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)*)\s*\(`)

	tsClassRegex     = regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+([a-zA-Z0-9_]+)(?:\s+extends\s+([a-zA-Z0-9_.]+))?(?:\s+implements\s+([a-zA-Z0-9_., ]+))?`)
	tsInterfaceRegex = regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?interface\s+([a-zA-Z0-9_]+)(?:\s+extends\s+([a-zA-Z0-9_., ]+))?`)
	tsTypeRegex      = regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?type\s+([a-zA-Z0-9_]+)\s*(=|<)`)
	tsFuncRegex      = regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s+([a-zA-Z0-9_]+)\s*(?:<.*?>)?\s*\((.*?)\)(?:\s*:\s*(.*?))?`)
	tsArrowFuncRegex = regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?(?:const|let|var)\s+([a-zA-Z0-9_]+)\s*=\s*(?:async\s+)?\((.*?)\)(?:\s*:\s*(.*?))?\s*=>`)
	tsImportRegex    = regexp.MustCompile(`(?m)^[ \t]*(?:import\s+(?:(?:\{([^}]+)\}|\*\s+as\s+([a-zA-Z0-9_]+)|([a-zA-Z0-9_]+))\s+from\s+)?['"](.*?)['"]|const\s+.*?\s*=\s*require\(['"](.*?)['"]\))`)
	tsCallRegex      = regexp.MustCompile(`\b([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)*)\s*\(`)

	rsFnRegex     = regexp.MustCompile(`(?m)^[ \t]*(?:pub(?:\(.*?\))?\s+)?(?:async\s+)?(?:const\s+)?fn\s+([a-zA-Z0-9_]+)\s*(?:<.*?>)?\s*\((.*?)\)(?:\s*->\s*(.*?))?`)
	rsStructRegex = regexp.MustCompile(`(?m)^[ \t]*(?:pub(?:\(.*?\))?\s+)?struct\s+([a-zA-Z0-9_]+)`)
	rsEnumRegex   = regexp.MustCompile(`(?m)^[ \t]*(?:pub(?:\(.*?\))?\s+)?enum\s+([a-zA-Z0-9_]+)`)
	rsTraitRegex  = regexp.MustCompile(`(?m)^[ \t]*(?:pub(?:\(.*?\))?\s+)?trait\s+([a-zA-Z0-9_]+)`)
	rsImplRegex   = regexp.MustCompile(`(?m)^[ \t]*impl(?:\s*<.*?>)?\s+(?:([a-zA-Z0-9_:]+)\s+for\s+)?([a-zA-Z0-9_:]+)`)
	rsUseRegex    = regexp.MustCompile(`(?m)^[ \t]*(?:pub\s+)?use\s+([a-zA-Z0-9_:]+(?:\{.*?\})?);`)

	protoMsgRegex     = regexp.MustCompile(`(?m)^[ \t]*message\s+([a-zA-Z0-9_]+)`)
	protoEnumRegex    = regexp.MustCompile(`(?m)^[ \t]*enum\s+([a-zA-Z0-9_]+)`)
	protoServiceRegex = regexp.MustCompile(`(?m)^[ \t]*service\s+([a-zA-Z0-9_]+)`)
	protoRpcRegex     = regexp.MustCompile(`(?m)^[ \t]*rpc\s+([a-zA-Z0-9_]+)\s*\((.*?)\)\s*returns\s*\((.*?)\)`)
	protoImportRegex  = regexp.MustCompile(`(?m)^[ \t]*import\s+(?:public\s+|weak\s+)?['"](.*?)['"]`)
)

func parsePythonFile(relPath, blobHash string, content []byte) ([]Node, []Edge, error) {
	text := string(content)
	lines := strings.Split(text, "\n")
	var nodes []Node
	var edges []Edge

	currentClass := ""
	currentClassIndent := -1

	for lineIdx, line := range lines {
		lineNum := lineIdx + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// Check if we exited current class
		if currentClass != "" && indent <= currentClassIndent && currentClassIndent >= 0 {
			currentClass = ""
			currentClassIndent = -1
		}

		// Docstring inspection (look ahead for triple quotes)
		doc := extractPythonDocstring(lines, lineIdx)

		// 1. Classes
		if m := pyClassRegex.FindStringSubmatch(line); len(m) > 1 {
			className := m[1]
			currentClass = className
			currentClassIndent = indent
			symbolID := fmt.Sprintf("%s:%s", relPath, className)

			nodes = append(nodes, Node{
				SymbolID:  symbolID,
				BlobHash:  blobHash,
				Kind:      "class",
				Name:      className,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: fmt.Sprintf("class %s", className),
				Docstring: doc,
				Exported:  !strings.HasPrefix(className, "_"),
			})

			if len(m) > 2 && m[2] != "" {
				bases := strings.Split(m[2], ",")
				for _, base := range bases {
					b := strings.TrimSpace(base)
					if b != "" && b != "object" {
						edges = append(edges, Edge{
							BlobHash:     blobHash,
							SourceSymbol: symbolID,
							TargetSymbol: b,
							Relation:     "inherits",
							FilePath:     relPath,
							Line:         lineNum,
						})
					}
				}
			}
		}

		// 2. Functions & Methods
		if m := pyFuncRegex.FindStringSubmatch(line); len(m) > 2 {
			fnName := m[2]
			params := m[3]
			retType := ""
			if len(m) > 4 {
				retType = m[4]
			}

			kind := "function"
			symbolID := fmt.Sprintf("%s:%s", relPath, fnName)
			sig := fmt.Sprintf("def %s(%s)", fnName, params)
			if retType != "" {
				sig += fmt.Sprintf(" -> %s", strings.TrimSpace(retType))
			}

			if currentClass != "" && indent > currentClassIndent {
				kind = "method"
				symbolID = fmt.Sprintf("%s:%s.%s", relPath, currentClass, fnName)
			}

			nodes = append(nodes, Node{
				SymbolID:  symbolID,
				BlobHash:  blobHash,
				Kind:      kind,
				Name:      fnName,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: sig,
				Docstring: doc,
				Exported:  !strings.HasPrefix(fnName, "_"),
			})

			// Extract call edges within function
			callMatches := pyCallRegex.FindAllStringSubmatch(line, -1)
			for _, cm := range callMatches {
				callee := cm[1]
				if callee != "def" && callee != "class" && callee != "if" && callee != "return" && callee != fnName {
					edges = append(edges, Edge{
						BlobHash:     blobHash,
						SourceSymbol: symbolID,
						TargetSymbol: callee,
						Relation:     "calls",
						FilePath:     relPath,
						Line:         lineNum,
					})
				}
			}
		}

		// 3. Imports
		if m := pyImportRegex.FindStringSubmatch(line); len(m) > 0 {
			target := m[1]
			if target == "" && len(m) > 3 {
				target = m[3]
			}
			if target != "" {
				edges = append(edges, Edge{
					BlobHash:     blobHash,
					SourceSymbol: relPath,
					TargetSymbol: strings.TrimSpace(target),
					Relation:     "imports",
					FilePath:     relPath,
					Line:         lineNum,
				})
			}
		}
	}

	return nodes, edges, nil
}

func extractPythonDocstring(lines []string, startIdx int) string {
	if startIdx+1 >= len(lines) {
		return ""
	}
	nextLine := strings.TrimSpace(lines[startIdx+1])
	if strings.HasPrefix(nextLine, `"""`) || strings.HasPrefix(nextLine, `'''`) {
		quote := nextLine[:3]
		docContent := strings.TrimPrefix(nextLine, quote)
		if strings.HasSuffix(docContent, quote) {
			return strings.TrimSuffix(docContent, quote)
		}
		var sb strings.Builder
		sb.WriteString(docContent)
		for i := startIdx + 2; i < len(lines); i++ {
			cur := strings.TrimSpace(lines[i])
			if strings.HasSuffix(cur, quote) {
				sb.WriteString(" " + strings.TrimSuffix(cur, quote))
				break
			}
			sb.WriteString(" " + cur)
		}
		return strings.TrimSpace(sb.String())
	}
	return ""
}

func parseJSOrTSFile(relPath, blobHash string, content []byte) ([]Node, []Edge, error) {
	text := string(content)
	lines := strings.Split(text, "\n")
	var nodes []Node
	var edges []Edge

	for lineIdx, line := range lines {
		lineNum := lineIdx + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		doc := extractJSDoc(lines, lineIdx)
		isExported := strings.Contains(line, "export")

		// 1. Interfaces
		if m := tsInterfaceRegex.FindStringSubmatch(line); len(m) > 1 {
			ifaceName := m[1]
			symbolID := fmt.Sprintf("%s:%s", relPath, ifaceName)
			nodes = append(nodes, Node{
				SymbolID:  symbolID,
				BlobHash:  blobHash,
				Kind:      "interface",
				Name:      ifaceName,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: fmt.Sprintf("interface %s", ifaceName),
				Docstring: doc,
				Exported:  isExported,
			})
			if len(m) > 2 && m[2] != "" {
				for _, ext := range strings.Split(m[2], ",") {
					ext = strings.TrimSpace(ext)
					if ext != "" {
						edges = append(edges, Edge{
							BlobHash:     blobHash,
							SourceSymbol: symbolID,
							TargetSymbol: ext,
							Relation:     "inherits",
							FilePath:     relPath,
							Line:         lineNum,
						})
					}
				}
			}
		}

		// 2. Types
		if m := tsTypeRegex.FindStringSubmatch(line); len(m) > 1 {
			typeName := m[1]
			symbolID := fmt.Sprintf("%s:%s", relPath, typeName)
			nodes = append(nodes, Node{
				SymbolID:  symbolID,
				BlobHash:  blobHash,
				Kind:      "type",
				Name:      typeName,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: fmt.Sprintf("type %s", typeName),
				Docstring: doc,
				Exported:  isExported,
			})
		}

		// 3. Classes
		if m := tsClassRegex.FindStringSubmatch(line); len(m) > 1 {
			className := m[1]
			symbolID := fmt.Sprintf("%s:%s", relPath, className)
			nodes = append(nodes, Node{
				SymbolID:  symbolID,
				BlobHash:  blobHash,
				Kind:      "class",
				Name:      className,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: fmt.Sprintf("class %s", className),
				Docstring: doc,
				Exported:  isExported,
			})
			if len(m) > 2 && m[2] != "" {
				edges = append(edges, Edge{
					BlobHash:     blobHash,
					SourceSymbol: symbolID,
					TargetSymbol: strings.TrimSpace(m[2]),
					Relation:     "inherits",
					FilePath:     relPath,
					Line:         lineNum,
				})
			}
			if len(m) > 3 && m[3] != "" {
				for _, iface := range strings.Split(m[3], ",") {
					iface = strings.TrimSpace(iface)
					if iface != "" {
						edges = append(edges, Edge{
							BlobHash:     blobHash,
							SourceSymbol: symbolID,
							TargetSymbol: iface,
							Relation:     "implements",
							FilePath:     relPath,
							Line:         lineNum,
						})
					}
				}
			}
		}

		// 4. Functions (Standard & Arrow)
		var fnName, params, retType string
		if m := tsFuncRegex.FindStringSubmatch(line); len(m) > 1 {
			fnName = m[1]
			if len(m) > 2 {
				params = m[2]
			}
			if len(m) > 3 {
				retType = m[3]
			}
		} else if m := tsArrowFuncRegex.FindStringSubmatch(line); len(m) > 1 {
			fnName = m[1]
			if len(m) > 2 {
				params = m[2]
			}
			if len(m) > 3 {
				retType = m[3]
			}
		}

		if fnName != "" {
			symbolID := fmt.Sprintf("%s:%s", relPath, fnName)
			sig := fmt.Sprintf("function %s(%s)", fnName, params)
			if retType != "" {
				sig += fmt.Sprintf(": %s", strings.TrimSpace(retType))
			}

			nodes = append(nodes, Node{
				SymbolID:  symbolID,
				BlobHash:  blobHash,
				Kind:      "function",
				Name:      fnName,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: sig,
				Docstring: doc,
				Exported:  isExported,
			})

			// Call detection
			callMatches := tsCallRegex.FindAllStringSubmatch(line, -1)
			for _, cm := range callMatches {
				callee := cm[1]
				if callee != "function" && callee != "if" && callee != "return" && callee != "catch" && callee != fnName {
					edges = append(edges, Edge{
						BlobHash:     blobHash,
						SourceSymbol: symbolID,
						TargetSymbol: callee,
						Relation:     "calls",
						FilePath:     relPath,
						Line:         lineNum,
					})
				}
			}
		}

		// 5. Imports
		if m := tsImportRegex.FindStringSubmatch(line); len(m) > 0 {
			target := ""
			for i := len(m) - 1; i >= 1; i-- {
				if m[i] != "" && !strings.Contains(m[i], ",") {
					target = m[i]
					break
				}
			}
			if target != "" {
				edges = append(edges, Edge{
					BlobHash:     blobHash,
					SourceSymbol: relPath,
					TargetSymbol: target,
					Relation:     "imports",
					FilePath:     relPath,
					Line:         lineNum,
				})
			}
		}
	}

	return nodes, edges, nil
}

func extractJSDoc(lines []string, targetIdx int) string {
	if targetIdx == 0 {
		return ""
	}
	prev := strings.TrimSpace(lines[targetIdx-1])
	if strings.HasSuffix(prev, "*/") {
		var docLines []string
		for i := targetIdx - 1; i >= 0; i-- {
			l := strings.TrimSpace(lines[i])
			clean := strings.TrimPrefix(l, "/**")
			clean = strings.TrimPrefix(clean, "/*")
			clean = strings.TrimSuffix(clean, "*/")
			clean = strings.TrimPrefix(clean, "*")
			clean = strings.TrimSpace(clean)
			if clean != "" {
				docLines = append([]string{clean}, docLines...)
			}
			if strings.HasPrefix(l, "/**") || strings.HasPrefix(l, "/*") {
				break
			}
		}
		return strings.Join(docLines, " ")
	}
	return ""
}

func parseRustFile(relPath, blobHash string, content []byte) ([]Node, []Edge, error) {
	text := string(content)
	lines := strings.Split(text, "\n")
	var nodes []Node
	var edges []Edge

	for lineIdx, line := range lines {
		lineNum := lineIdx + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "///") {
			continue
		}

		isPub := strings.Contains(line, "pub")

		// 1. Functions
		if m := rsFnRegex.FindStringSubmatch(line); len(m) > 1 {
			fnName := m[1]
			params := ""
			ret := ""
			if len(m) > 2 {
				params = m[2]
			}
			if len(m) > 3 {
				ret = m[3]
			}
			symbolID := fmt.Sprintf("%s:%s", relPath, fnName)
			sig := fmt.Sprintf("fn %s(%s)", fnName, params)
			if ret != "" {
				sig += fmt.Sprintf(" -> %s", strings.TrimSpace(ret))
			}

			nodes = append(nodes, Node{
				SymbolID:  symbolID,
				BlobHash:  blobHash,
				Kind:      "function",
				Name:      fnName,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: sig,
				Exported:  isPub,
			})
		}

		// 2. Structs
		if m := rsStructRegex.FindStringSubmatch(line); len(m) > 1 {
			name := m[1]
			nodes = append(nodes, Node{
				SymbolID:  fmt.Sprintf("%s:%s", relPath, name),
				BlobHash:  blobHash,
				Kind:      "struct",
				Name:      name,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: fmt.Sprintf("struct %s", name),
				Exported:  isPub,
			})
		}

		// 3. Enums
		if m := rsEnumRegex.FindStringSubmatch(line); len(m) > 1 {
			name := m[1]
			nodes = append(nodes, Node{
				SymbolID:  fmt.Sprintf("%s:%s", relPath, name),
				BlobHash:  blobHash,
				Kind:      "enum",
				Name:      name,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: fmt.Sprintf("enum %s", name),
				Exported:  isPub,
			})
		}

		// 4. Traits
		if m := rsTraitRegex.FindStringSubmatch(line); len(m) > 1 {
			name := m[1]
			nodes = append(nodes, Node{
				SymbolID:  fmt.Sprintf("%s:%s", relPath, name),
				BlobHash:  blobHash,
				Kind:      "trait",
				Name:      name,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: fmt.Sprintf("trait %s", name),
				Exported:  isPub,
			})
		}

		// 5. Impl blocks
		if m := rsImplRegex.FindStringSubmatch(line); len(m) > 2 {
			trait := m[1]
			target := m[2]
			if trait != "" {
				edges = append(edges, Edge{
					BlobHash:     blobHash,
					SourceSymbol: fmt.Sprintf("%s:%s", relPath, target),
					TargetSymbol: trait,
					Relation:     "implements",
					FilePath:     relPath,
					Line:         lineNum,
				})
			}
		}

		// 6. Use / imports
		if m := rsUseRegex.FindStringSubmatch(line); len(m) > 1 {
			edges = append(edges, Edge{
				BlobHash:     blobHash,
				SourceSymbol: relPath,
				TargetSymbol: m[1],
				Relation:     "imports",
				FilePath:     relPath,
				Line:         lineNum,
			})
		}
	}

	return nodes, edges, nil
}

func parseProtoFile(relPath, blobHash string, content []byte) ([]Node, []Edge, error) {
	text := string(content)
	lines := strings.Split(text, "\n")
	var nodes []Node
	var edges []Edge

	for lineIdx, line := range lines {
		lineNum := lineIdx + 1

		if m := protoMsgRegex.FindStringSubmatch(line); len(m) > 1 {
			name := m[1]
			nodes = append(nodes, Node{
				SymbolID:  fmt.Sprintf("%s:%s", relPath, name),
				BlobHash:  blobHash,
				Kind:      "message",
				Name:      name,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: fmt.Sprintf("message %s", name),
				Exported:  true,
			})
		}

		if m := protoEnumRegex.FindStringSubmatch(line); len(m) > 1 {
			name := m[1]
			nodes = append(nodes, Node{
				SymbolID:  fmt.Sprintf("%s:%s", relPath, name),
				BlobHash:  blobHash,
				Kind:      "enum",
				Name:      name,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: fmt.Sprintf("enum %s", name),
				Exported:  true,
			})
		}

		if m := protoServiceRegex.FindStringSubmatch(line); len(m) > 1 {
			name := m[1]
			nodes = append(nodes, Node{
				SymbolID:  fmt.Sprintf("%s:%s", relPath, name),
				BlobHash:  blobHash,
				Kind:      "service",
				Name:      name,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: fmt.Sprintf("service %s", name),
				Exported:  true,
			})
		}

		if m := protoRpcRegex.FindStringSubmatch(line); len(m) > 3 {
			rpcName := m[1]
			reqType := m[2]
			respType := m[3]
			symbolID := fmt.Sprintf("%s:%s", relPath, rpcName)
			nodes = append(nodes, Node{
				SymbolID:  symbolID,
				BlobHash:  blobHash,
				Kind:      "rpc",
				Name:      rpcName,
				FilePath:  relPath,
				StartLine: lineNum,
				EndLine:   lineNum,
				Signature: strings.TrimSpace(line),
				Exported:  true,
			})
			edges = append(edges, Edge{
				BlobHash:     blobHash,
				SourceSymbol: symbolID,
				TargetSymbol: strings.TrimSpace(reqType),
				Relation:     "references",
				FilePath:     relPath,
				Line:         lineNum,
			})
			edges = append(edges, Edge{
				BlobHash:     blobHash,
				SourceSymbol: symbolID,
				TargetSymbol: strings.TrimSpace(respType),
				Relation:     "references",
				FilePath:     relPath,
				Line:         lineNum,
			})
		}

		if m := protoImportRegex.FindStringSubmatch(line); len(m) > 1 {
			edges = append(edges, Edge{
				BlobHash:     blobHash,
				SourceSymbol: relPath,
				TargetSymbol: m[1],
				Relation:     "imports",
				FilePath:     relPath,
				Line:         lineNum,
			})
		}
	}

	return nodes, edges, nil
}

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	r := rune(name[0])
	return unicode.IsUpper(r)
}

func formatGoFuncSig(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recvType := formatGoExpr(d.Recv.List[0].Type)
		b.WriteString(fmt.Sprintf("(%s) ", recvType))
	}
	b.WriteString(d.Name.Name)
	b.WriteString("(")
	if d.Type.Params != nil {
		for i, p := range d.Type.Params.List {
			if i > 0 {
				b.WriteString(", ")
			}
			pType := formatGoExpr(p.Type)
			if len(p.Names) > 0 {
				b.WriteString(p.Names[0].Name + " ")
			}
			b.WriteString(pType)
		}
	}
	b.WriteString(")")
	if d.Type.Results != nil {
		b.WriteString(" ")
		if len(d.Type.Results.List) > 1 {
			b.WriteString("(")
		}
		for i, r := range d.Type.Results.List {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(formatGoExpr(r.Type))
		}
		if len(d.Type.Results.List) > 1 {
			b.WriteString(")")
		}
	}
	return b.String()
}

func formatGoExpr(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", formatGoExpr(e.X), e.Sel.Name)
	case *ast.StarExpr:
		return fmt.Sprintf("*%s", formatGoExpr(e.X))
	case *ast.ArrayType:
		return fmt.Sprintf("[]%s", formatGoExpr(e.Elt))
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", formatGoExpr(e.Key), formatGoExpr(e.Value))
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return fmt.Sprintf("...%s", formatGoExpr(e.Elt))
	default:
		return ""
	}
}
