package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestBuildFileIndex_PrimaryOwnsProductionDeterministic locks ADR-0002: a
// production file present in both a primary build package and its test variant
// must always be owned by the primary, regardless of packages.Load order (which
// is not guaranteed). The test-variant must own only the _test.go file.
func TestBuildFileIndex_PrimaryOwnsProductionDeterministic(t *testing.T) {
	fset := token.NewFileSet()
	mustParse := func(name, src string) *ast.File {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		return f
	}
	prodPrimary := mustParse("prod.go", "package p\nfunc F() {}\n")
	prodInVariant := mustParse("prod_variant.go", "package p\nfunc F() {}\n")
	testFile := mustParse("prod_test.go", "package p\nfunc TestF() {}\n")

	primaryInfo := &types.Info{}
	variantInfo := &types.Info{}

	primary := &packages.Package{
		ID:              "example.com/p",
		Fset:            fset,
		TypesInfo:       primaryInfo,
		Syntax:          []*ast.File{prodPrimary},
		CompiledGoFiles: []string{"prod.go"},
	}
	// Test variant re-includes the production file (prod.go) plus the _test.go.
	variant := &packages.Package{
		ID:              "example.com/p [example.com/p.test]",
		Fset:            fset,
		TypesInfo:       variantInfo,
		Syntax:          []*ast.File{prodInVariant, testFile},
		CompiledGoFiles: []string{"prod.go", "prod_test.go"},
	}

	if !isTestVariantPkg(variant) {
		t.Fatalf("variant should be classified as a test variant: %q", variant.ID)
	}
	if isTestVariantPkg(primary) {
		t.Fatalf("primary should NOT be a test variant: %q", primary.ID)
	}

	// Both load orders must yield the identical ownership.
	for _, tc := range []struct {
		name string
		pkgs []*packages.Package
	}{
		{"primary-first", []*packages.Package{primary, variant}},
		{"variant-first", []*packages.Package{variant, primary}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx := buildFileIndex(tc.pkgs)
			if got := idx["prod.go"].info; got != primaryInfo {
				t.Errorf("prod.go owned by wrong package: got %p, want primary %p", got, primaryInfo)
			}
			if got := idx["prod_test.go"].info; got != variantInfo {
				t.Errorf("prod_test.go should come from the test variant: got %p, want %p", got, variantInfo)
			}
		})
	}
}
