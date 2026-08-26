package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRepositoryFacadeIsCurrentAndExportComplete(t *testing.T) {
	t.Parallel()

	repositoryRoot := repositoryRootFromCaller(t)
	sourceDirectory := filepath.Join(repositoryRoot, defaultSourceDirectory)
	facadePath := filepath.Join(repositoryRoot, defaultOutputPath)

	generated, err := generateFacade(generatorConfig{
		sourceDirectory:      sourceDirectory,
		packageName:          defaultPackageName,
		implementationImport: defaultImplementationImport,
	})
	if err != nil {
		t.Fatalf("generate repository facade: %v", err)
	}
	committed, err := os.ReadFile(facadePath)
	if err != nil {
		t.Fatalf("read %s: %v", facadePath, err)
	}
	if !bytes.Equal(committed, generated) {
		t.Fatalf("%s is stale; run go run ./internal/cmd/apifacade", facadePath)
	}

	sourceFiles, sourceFileSet, err := parseProductionFiles(sourceDirectory)
	if err != nil {
		t.Fatalf("parse implementation package: %v", err)
	}
	exports, err := collectExports(sourceFiles, sourceFileSet)
	if err != nil {
		t.Fatalf("collect implementation exports: %v", err)
	}
	facadeExports, err := parseFacadeExportKinds(facadePath, committed)
	if err != nil {
		t.Fatalf("parse facade exports: %v", err)
	}
	if len(facadeExports) != len(exports.allNames) {
		t.Fatalf("facade export count = %d, implementation export count = %d", len(facadeExports), len(exports.allNames))
	}
	for name, wantKind := range exports.allNames {
		if gotKind, ok := facadeExports[name]; !ok {
			t.Errorf("facade is missing exported %s %s", wantKind, name)
		} else if gotKind != wantKind {
			t.Errorf("facade exports %s as %s, want %s", name, gotKind, wantKind)
		}
	}
	for name, kind := range facadeExports {
		if _, ok := exports.allNames[name]; !ok {
			t.Errorf("facade has unexpected exported %s %s", kind, name)
		}
	}
}

func repositoryRootFromCaller(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repository test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func parseFacadeExportKinds(filename string, source []byte) (map[string]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), filename, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	exports := make(map[string]string)
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.GenDecl:
			kind := ""
			switch typed.Tok {
			case token.TYPE:
				kind = "type"
			case token.CONST:
				kind = "const"
			case token.VAR:
				kind = "var"
			default:
				continue
			}
			for _, specification := range typed.Specs {
				switch spec := specification.(type) {
				case *ast.TypeSpec:
					if ast.IsExported(spec.Name.Name) {
						if !spec.Assign.IsValid() {
							return nil, fmt.Errorf("exported type %s is not an alias", spec.Name.Name)
						}
						exports[spec.Name.Name] = kind
					}
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						if ast.IsExported(name.Name) {
							exports[name.Name] = kind
						}
					}
				}
			}
		case *ast.FuncDecl:
			if typed.Recv == nil && ast.IsExported(typed.Name.Name) {
				exports[typed.Name.Name] = "func"
			}
		}
	}
	return exports, nil
}
