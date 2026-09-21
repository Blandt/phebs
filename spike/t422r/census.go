// Package t422r inventories Phebs-owned HTTP error constructors reachable
// from the four endpoints polled by the T40.13 ceremony inspector.
package t422r

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

const (
	apiPackagePath  = "github.com/bmeddeb/phebs/internal/api"
	humaPackagePath = "github.com/danielgtaylor/huma/v2"
	maximumRecords  = 4096
)

var endpointPaths = map[string]struct{}{
	"/api/repo-status":                {},
	"/api/observation-progress":       {},
	"/api/extraction-progress":        {},
	"/api/caller-generation-progress": {},
}

// Record is either a reachable Huma error constructor or a dynamic call that
// prevents the package-local traversal from proving a complete downstream
// call graph.
type Record struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Endpoint string `json:"endpoint"`
	Function string `json:"function"`
	File     string `json:"file"`
	Offset   int    `json:"offset"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`

	Status               int    `json:"status,omitempty"`
	Constructor          string `json:"constructor,omitempty"`
	DetailKind           string `json:"detail_kind,omitempty"`
	Detail               string `json:"detail,omitempty"`
	StaticClassification string `json:"static_classification,omitempty"`
	BoundaryKind         string `json:"boundary_kind,omitempty"`
	Boundary             string `json:"boundary,omitempty"`
}

type header struct {
	Kind                 string   `json:"kind"`
	Schema               string   `json:"schema"`
	SourceCommit         string   `json:"source_commit"`
	GoVersion            string   `json:"go_version"`
	GOOS                 string   `json:"goos"`
	GOARCH               string   `json:"goarch"`
	Roots                []string `json:"roots"`
	ErrorSites           int      `json:"error_sites"`
	UnresolvedBoundaries int      `json:"unresolved_boundaries"`
	RealFault            int      `json:"real_fault"`
	UnnameableInline     int      `json:"unnameable_inline"`
	Candidates           int      `json:"candidates"`
}

type functionBody struct {
	decl  *ast.FuncDecl
	label string
}

type scanner struct {
	root         string
	sourceCommit string
	pkg          *packages.Package
	functions    map[*types.Func]functionBody
	records      map[string]Record
}

// Census loads internal/api without network access and returns a stable,
// endpoint-qualified inventory.
func Census(ctx context.Context, root, sourceCommit string) ([]Record, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if !validCommit(sourceCommit) {
		return nil, errors.New("source commit must be a lowercase 40-character hexadecimal identity")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root symlinks: %w", err)
	}
	tracked, err := readCommitTree(ctx, absolute, sourceCommit)
	if err != nil {
		return nil, fmt.Errorf("read pinned source tree: %w", err)
	}
	if err := verifyTrackedInputs(absolute, tracked); err != nil {
		return nil, fmt.Errorf("verify pinned source inputs: %w", err)
	}
	loaded, err := packages.Load(&packages.Config{
		Context: ctx,
		Dir:     absolute,
		Env:     offlinePackageEnv(),
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
	}, "./internal/api")
	if err != nil {
		return nil, fmt.Errorf("load internal/api: %w", err)
	}
	if len(loaded) != 1 {
		return nil, fmt.Errorf("load internal/api: got %d packages, want 1", len(loaded))
	}
	pkg := loaded[0]
	if len(pkg.Errors) != 0 || pkg.IllTyped || pkg.Types == nil || pkg.TypesInfo == nil || pkg.Fset == nil {
		return nil, fmt.Errorf("internal/api is not type-complete: %v", pkg.Errors)
	}
	if pkg.PkgPath != apiPackagePath || len(pkg.CompiledGoFiles) != len(pkg.Syntax) {
		return nil, fmt.Errorf("internal/api source/type mismatch: package=%q files=%d syntax=%d", pkg.PkgPath, len(pkg.CompiledGoFiles), len(pkg.Syntax))
	}
	if err := verifyCompiledInputs(absolute, tracked, pkg.CompiledGoFiles); err != nil {
		return nil, fmt.Errorf("verify compiled Go inputs: %w", err)
	}

	s := &scanner{
		root:         absolute,
		sourceCommit: sourceCommit,
		pkg:          pkg,
		functions:    make(map[*types.Func]functionBody),
		records:      make(map[string]Record),
	}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			object, ok := pkg.TypesInfo.Defs[function.Name].(*types.Func)
			if !ok {
				return nil, fmt.Errorf("resolve function declaration %s", function.Name.Name)
			}
			s.functions[object] = functionBody{decl: function, label: functionLabel(object)}
		}
	}

	roots, err := s.routeRoots()
	if err != nil {
		return nil, err
	}
	for _, endpoint := range sortedEndpoints(roots) {
		if err := s.scanEndpoint(endpoint, roots[endpoint]); err != nil {
			return nil, err
		}
	}
	if err := verifyTrackedInputs(absolute, tracked); err != nil {
		return nil, fmt.Errorf("reverify pinned source inputs: %w", err)
	}
	records := make([]Record, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return recordKey(records[i]) < recordKey(records[j]) })
	return records, nil
}

// EncodeJSONL writes one deterministic JSON object per line.
func EncodeJSONL(writer io.Writer, sourceCommit string, records []Record) error {
	if !validCommit(sourceCommit) {
		return errors.New("source commit must be a lowercase 40-character hexadecimal identity")
	}
	for _, record := range records {
		if record.ID != recordID(sourceCommit, record) {
			return fmt.Errorf("census record %q is not bound to source commit", record.ID)
		}
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(censusHeader(sourceCommit, records)); err != nil {
		return fmt.Errorf("encode census header: %w", err)
	}
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return fmt.Errorf("encode census record: %w", err)
		}
	}
	return nil
}

func (s *scanner) routeRoots() (map[string]*ast.FuncLit, error) {
	roots := make(map[string]*ast.FuncLit, len(endpointPaths))
	counts := make(map[string]int, len(endpointPaths))
	for _, file := range s.pkg.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 3 {
				return true
			}
			path, ok := routePath(s.pkg.TypesInfo, call)
			if !ok {
				return true
			}
			if _, wanted := endpointPaths[path]; !wanted {
				return true
			}
			counts[path]++
			handler, ok := call.Args[2].(*ast.FuncLit)
			if !ok || handler.Body == nil {
				roots[path] = nil
				return true
			}
			roots[path] = handler
			return true
		})
	}
	for endpoint := range endpointPaths {
		if roots[endpoint] == nil || counts[endpoint] != 1 {
			return nil, fmt.Errorf("resolve exactly one typed handler for %s", endpoint)
		}
	}
	return roots, nil
}

func routePath(info *types.Info, call *ast.CallExpr) (string, bool) {
	object := callObject(info, call.Fun)
	if isFunction(object, humaPackagePath, "Get") {
		return constantString(info, call.Args[1])
	}
	if !isFunction(object, humaPackagePath, "Register") {
		return "", false
	}
	operation, ok := call.Args[1].(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	var path, method string
	for _, element := range operation.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, ok := field.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch name.Name {
		case "Path":
			path, _ = constantString(info, field.Value)
		case "Method":
			method, _ = constantString(info, field.Value)
		}
	}
	return path, path != "" && method == "GET"
}

func (s *scanner) scanEndpoint(endpoint string, root *ast.FuncLit) error {
	queue := make([]functionBody, 0, 32)
	seen := make(map[*types.Func]struct{})
	if err := s.scanBody(endpoint, "route "+endpoint, root.Body, &queue, seen); err != nil {
		return err
	}
	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]
		if err := s.scanBody(endpoint, current.label, current.decl.Body, &queue, seen); err != nil {
			return err
		}
	}
	return nil
}

func (s *scanner) scanBody(
	endpoint string,
	function string,
	body *ast.BlockStmt,
	queue *[]functionBody,
	seen map[*types.Func]struct{},
) error {
	var scanErr error
	ast.Inspect(body, func(node ast.Node) bool {
		if scanErr != nil {
			return false
		}
		if closure, ok := node.(*ast.FuncLit); ok {
			record, err := s.boundaryAt(endpoint, function, closure.Pos(), "inline_closure", "inline closure")
			if err != nil {
				scanErr = err
				return false
			}
			if err := s.add(record); err != nil {
				scanErr = err
			}
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		object := callObject(s.pkg.TypesInfo, call.Fun)
		if err := s.scanFunctionArguments(endpoint, function, call, queue, seen); err != nil {
			scanErr = err
			return false
		}
		if status, ok := humaErrorStatus(object); ok {
			record, err := s.errorRecord(endpoint, function, call, object, status)
			if err != nil {
				scanErr = err
				return false
			}
			if err := s.add(record); err != nil {
				scanErr = err
				return false
			}
			return true
		}
		if isInterfaceCall(s.pkg.TypesInfo, call.Fun) {
			target := "interface method"
			if object != nil {
				target = functionLabel(object)
			}
			record, err := s.boundaryRecord(endpoint, function, call, "interface_dispatch", target)
			if err != nil {
				scanErr = err
				return false
			}
			if err := s.add(record); err != nil {
				scanErr = err
				return false
			}
			return true
		}
		if object != nil && object.Pkg() == s.pkg.Types {
			if body, ok := s.functions[object]; ok {
				if _, visited := seen[object]; !visited {
					seen[object] = struct{}{}
					*queue = append(*queue, body)
				}
				return true
			}
			record, err := s.boundaryRecord(endpoint, function, call, "missing_local_body", functionLabel(object))
			if err != nil {
				scanErr = err
				return false
			}
			if err := s.add(record); err != nil {
				scanErr = err
				return false
			}
			return true
		}
		if object != nil && isPhebsPackage(object.Pkg()) {
			record, err := s.boundaryRecord(endpoint, function, call, "phebs_package_call", qualifiedFunctionLabel(object))
			if err != nil {
				scanErr = err
				return false
			}
			if err := s.add(record); err != nil {
				scanErr = err
				return false
			}
			return true
		}
		if object == nil && !isBuiltinCall(s.pkg.TypesInfo, call.Fun) && isFunctionCall(s.pkg.TypesInfo, call.Fun) {
			if _, inline := call.Fun.(*ast.FuncLit); inline {
				return true
			}
			target, err := formatNode(s.pkg.Fset, call.Fun)
			if err != nil {
				scanErr = err
				return false
			}
			record, err := s.boundaryRecord(endpoint, function, call, "function_value", target)
			if err != nil {
				scanErr = err
				return false
			}
			if err := s.add(record); err != nil {
				scanErr = err
				return false
			}
		}
		return true
	})
	return scanErr
}

func (s *scanner) scanFunctionArguments(
	endpoint string,
	function string,
	call *ast.CallExpr,
	queue *[]functionBody,
	seen map[*types.Func]struct{},
) error {
	for _, argument := range call.Args {
		if _, inline := argument.(*ast.FuncLit); inline || !isFunctionCall(s.pkg.TypesInfo, argument) {
			continue
		}
		object := callObject(s.pkg.TypesInfo, argument)
		if object != nil && object.Pkg() == s.pkg.Types && !isInterfaceCall(s.pkg.TypesInfo, argument) {
			if body, ok := s.functions[object]; ok {
				if _, visited := seen[object]; !visited {
					seen[object] = struct{}{}
					*queue = append(*queue, body)
				}
				continue
			}
			record, err := s.boundaryAt(endpoint, function, argument.Pos(), "missing_local_body", functionLabel(object))
			if err != nil {
				return err
			}
			if err := s.add(record); err != nil {
				return err
			}
			continue
		}
		target, err := formatNode(s.pkg.Fset, argument)
		if err != nil {
			return err
		}
		kind := "function_argument"
		if object != nil && isPhebsPackage(object.Pkg()) {
			kind = "phebs_package_callback"
			target = qualifiedFunctionLabel(object)
		}
		record, err := s.boundaryAt(endpoint, function, argument.Pos(), kind, target)
		if err != nil {
			return err
		}
		if err := s.add(record); err != nil {
			return err
		}
	}
	return nil
}

func (s *scanner) errorRecord(
	endpoint string,
	function string,
	call *ast.CallExpr,
	object *types.Func,
	status int,
) (Record, error) {
	record, err := s.positionedRecord(endpoint, "error_site", function, call.Pos())
	if err != nil {
		return Record{}, err
	}
	record.Status = status
	record.Constructor = object.Name()
	if len(call.Args) == 0 {
		record.DetailKind = "absent"
	} else if detail, ok := constantString(s.pkg.TypesInfo, call.Args[0]); ok {
		switch {
		case isNamedConstant(s.pkg.TypesInfo, call.Args[0]):
			record.DetailKind = "named_constant"
		case isStringLiteral(call.Args[0]):
			record.DetailKind = "inline_literal"
		default:
			record.DetailKind = "inline_constant"
		}
		record.Detail = detail
	} else {
		record.DetailKind = "expression"
		record.Detail, err = formatNode(s.pkg.Fset, call.Args[0])
		if err != nil {
			return Record{}, err
		}
	}
	record.StaticClassification = staticClassification(record.Status, record.DetailKind)
	record.ID = recordID(s.sourceCommit, record)
	return record, nil
}

func (s *scanner) boundaryRecord(
	endpoint string,
	function string,
	call *ast.CallExpr,
	kind string,
	target string,
) (Record, error) {
	return s.boundaryAt(endpoint, function, call.Pos(), kind, target)
}

func (s *scanner) boundaryAt(
	endpoint string,
	function string,
	position token.Pos,
	kind string,
	target string,
) (Record, error) {
	record, err := s.positionedRecord(endpoint, "unresolved_boundary", function, position)
	if err != nil {
		return Record{}, err
	}
	record.BoundaryKind = kind
	record.Boundary = target
	record.ID = recordID(s.sourceCommit, record)
	return record, nil
}

func (s *scanner) positionedRecord(endpoint, kind, function string, position token.Pos) (Record, error) {
	pos := s.pkg.Fset.PositionFor(position, false)
	path, err := filepath.Rel(s.root, pos.Filename)
	if err != nil {
		return Record{}, fmt.Errorf("resolve source position %q: %w", pos.Filename, err)
	}
	if path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return Record{}, fmt.Errorf("source position %q is outside the repository root", pos.Filename)
	}
	return Record{
		Kind: kind, Endpoint: endpoint, Function: function,
		File: filepath.ToSlash(path), Offset: pos.Offset, Line: pos.Line, Column: pos.Column,
	}, nil
}

func (s *scanner) add(record Record) error {
	if _, exists := s.records[record.ID]; exists {
		return nil
	}
	if len(s.records) >= maximumRecords {
		return fmt.Errorf("census exceeds its %d-record bound", maximumRecords)
	}
	s.records[record.ID] = record
	return nil
}

func offlinePackageEnv() []string {
	overrides := map[string]string{
		"CGO_ENABLED": "0", "GOENV": "off", "GOPROXY": "off",
		"GOFLAGS": "-mod=readonly -buildvcs=false", "GOOS": runtime.GOOS,
		"GOARCH": runtime.GOARCH, "GONOSUMDB": "*", "GOPRIVATE": "*",
		"GOSUMDB": "off", "GOTELEMETRY": "off", "GOTOOLCHAIN": "local", "GOWORK": "off",
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, overridden := overrides[key]; !overridden {
			environment = append(environment, value)
		}
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		environment = append(environment, key+"="+overrides[key])
	}
	return environment
}

func sortedEndpoints(roots map[string]*ast.FuncLit) []string {
	result := make([]string, 0, len(roots))
	for endpoint := range roots {
		result = append(result, endpoint)
	}
	sort.Strings(result)
	return result
}

func endpointNames() []string {
	result := make([]string, 0, len(endpointPaths))
	for endpoint := range endpointPaths {
		result = append(result, endpoint)
	}
	sort.Strings(result)
	return result
}

func callObject(info *types.Info, expression ast.Expr) *types.Func {
	switch selected := expression.(type) {
	case *ast.Ident:
		object, _ := info.Uses[selected].(*types.Func)
		return object
	case *ast.SelectorExpr:
		if selection := info.Selections[selected]; selection != nil {
			object, _ := selection.Obj().(*types.Func)
			return object
		}
		object, _ := info.Uses[selected.Sel].(*types.Func)
		return object
	case *ast.IndexExpr:
		return callObject(info, selected.X)
	case *ast.IndexListExpr:
		return callObject(info, selected.X)
	case *ast.ParenExpr:
		return callObject(info, selected.X)
	default:
		return nil
	}
}

func isFunction(object *types.Func, packagePath, name string) bool {
	return object != nil && object.Pkg() != nil && object.Pkg().Path() == packagePath && object.Name() == name
}

func humaErrorStatus(object *types.Func) (int, bool) {
	if object == nil || object.Pkg() == nil || object.Pkg().Path() != humaPackagePath {
		return 0, false
	}
	name := object.Name()
	if len(name) < len("Error400") || !strings.HasPrefix(name, "Error") {
		return 0, false
	}
	status, err := strconv.Atoi(name[len("Error") : len("Error")+3])
	return status, err == nil && status >= 400 && status <= 599
}

func constantString(info *types.Info, expression ast.Expr) (string, bool) {
	value := info.Types[expression].Value
	if value == nil || value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(value), true
}

func isNamedConstant(info *types.Info, expression ast.Expr) bool {
	switch selected := expression.(type) {
	case *ast.Ident:
		_, ok := info.Uses[selected].(*types.Const)
		return ok
	case *ast.SelectorExpr:
		_, ok := info.Uses[selected.Sel].(*types.Const)
		return ok
	case *ast.ParenExpr:
		return isNamedConstant(info, selected.X)
	default:
		return false
	}
}

func isStringLiteral(expression ast.Expr) bool {
	switch selected := expression.(type) {
	case *ast.BasicLit:
		return selected.Kind == token.STRING
	case *ast.ParenExpr:
		return isStringLiteral(selected.X)
	default:
		return false
	}
}

func staticClassification(status int, detailKind string) string {
	if status != 409 {
		return "real_fault"
	}
	if detailKind != "named_constant" {
		return "unnameable_inline"
	}
	return ""
}

func isInterfaceCall(info *types.Info, expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	selection := info.Selections[selector]
	if selection == nil {
		return false
	}
	typeOf := selection.Recv()
	if pointer, ok := typeOf.(*types.Pointer); ok {
		typeOf = pointer.Elem()
	}
	_, ok = typeOf.Underlying().(*types.Interface)
	return ok
}

func isFunctionCall(info *types.Info, expression ast.Expr) bool {
	typeOf := info.TypeOf(expression)
	if typeOf == nil {
		return false
	}
	_, ok := typeOf.Underlying().(*types.Signature)
	return ok
}

func isBuiltinCall(info *types.Info, expression ast.Expr) bool {
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = info.Uses[identifier].(*types.Builtin)
	return ok
}

func functionLabel(function *types.Func) string {
	if function == nil {
		return ""
	}
	signature, _ := function.Type().(*types.Signature)
	if signature == nil || signature.Recv() == nil {
		return function.Name()
	}
	receiver := types.TypeString(signature.Recv().Type(), func(pkg *types.Package) string {
		if pkg.Path() == apiPackagePath {
			return ""
		}
		return pkg.Name()
	})
	return "(" + receiver + ")." + function.Name()
}

func qualifiedFunctionLabel(function *types.Func) string {
	if function == nil || function.Pkg() == nil {
		return functionLabel(function)
	}
	return function.Pkg().Path() + "." + functionLabel(function)
}

func isPhebsPackage(pkg *types.Package) bool {
	if pkg == nil {
		return false
	}
	const module = "github.com/bmeddeb/phebs"
	return pkg.Path() == module || strings.HasPrefix(pkg.Path(), module+"/")
}

func formatNode(fileSet *token.FileSet, node ast.Node) (string, error) {
	var builder strings.Builder
	if err := format.Node(&builder, fileSet, node); err != nil {
		return "", fmt.Errorf("format source expression: %w", err)
	}
	return builder.String(), nil
}

func recordID(sourceCommit string, record Record) string {
	canonical := strings.Join([]string{
		sourceCommit, record.Kind, record.Endpoint, record.Function, record.File,
		strconv.Itoa(record.Offset), strconv.Itoa(record.Status), record.Constructor,
		record.DetailKind, record.Detail, record.StaticClassification, record.BoundaryKind, record.Boundary,
	}, "\x00")
	digest := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(digest[:16])
}

func validCommit(commit string) bool {
	if len(commit) != 40 || commit != strings.ToLower(commit) {
		return false
	}
	decoded, err := hex.DecodeString(commit)
	return err == nil && len(decoded) == 20
}

func censusHeader(sourceCommit string, records []Record) header {
	result := header{
		Kind: "metadata", Schema: "phebs-t422r-error-site-census/v1",
		SourceCommit: sourceCommit, GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Roots: endpointNames(),
	}
	for _, record := range records {
		switch record.Kind {
		case "unresolved_boundary":
			result.UnresolvedBoundaries++
		case "error_site":
			result.ErrorSites++
			switch record.StaticClassification {
			case "real_fault":
				result.RealFault++
			case "unnameable_inline":
				result.UnnameableInline++
			default:
				result.Candidates++
			}
		}
	}
	return result
}

func recordKey(record Record) string {
	return strings.Join([]string{
		record.Endpoint, record.Kind, record.File, fmt.Sprintf("%012d", record.Offset),
		record.Function, record.ID,
	}, "\x00")
}
