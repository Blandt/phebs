package t422r

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestTraversalQueuesNamedCallbackAndReportsOpaqueCalls(t *testing.T) {
	const source = `package api
func callback() {}
func missing()
func sink(func()) {}
func host(dynamic func()) {
	sink(callback)
	sink(dynamic)
	missing()
}`
	root := t.TempDir()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filepath.Join(root, "fixture.go"), source, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object), Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	checked, err := (&types.Config{}).Check(apiPackagePath, fileSet, []*ast.File{file}, info)
	if err != nil {
		t.Fatal(err)
	}
	pkg := &packages.Package{Fset: fileSet, Types: checked, TypesInfo: info, Syntax: []*ast.File{file}}
	s := scanner{
		root: root, sourceCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", pkg: pkg,
		functions: make(map[*types.Func]functionBody), records: make(map[string]Record),
	}
	var host *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		object := info.Defs[function.Name].(*types.Func)
		if function.Body != nil {
			s.functions[object] = functionBody{decl: function, label: functionLabel(object)}
		}
		if function.Name.Name == "host" {
			host = function
		}
	}
	queue := make([]functionBody, 0, 2)
	if err := s.scanBody("/synthetic", "host", host.Body, &queue, make(map[*types.Func]struct{})); err != nil {
		t.Fatal(err)
	}
	queuedCallback := false
	for _, function := range queue {
		queuedCallback = queuedCallback || function.label == "callback"
	}
	if !queuedCallback {
		t.Fatal("direct named callback was not queued")
	}
	gotKinds := make(map[string]int)
	for _, record := range s.records {
		gotKinds[record.BoundaryKind]++
	}
	if gotKinds["function_argument"] != 1 || gotKinds["missing_local_body"] != 1 {
		t.Fatalf("opaque boundary kinds = %v", gotKinds)
	}
}

func TestBoundsStopBeforeAdditionalAllocation(t *testing.T) {
	s := scanner{records: make(map[string]Record, maximumRecords)}
	for index := 0; index < maximumRecords; index++ {
		if err := s.add(Record{ID: strconv.Itoa(index)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.add(Record{ID: "overflow"}); err == nil || len(s.records) != maximumRecords {
		t.Fatalf("record bound result = error %v, records %d", err, len(s.records))
	}

	buffer := boundedBuffer{limit: 4}
	if written, err := buffer.Write([]byte("overflow")); !errors.Is(err, errOutputLimit) || written != 4 ||
		buffer.buffer.Len() != 4 || !buffer.overflow {
		t.Fatalf("bounded buffer = written %d error %v bytes %d overflow %t", written, err, buffer.buffer.Len(), buffer.overflow)
	}
}
