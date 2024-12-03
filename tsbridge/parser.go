package tsbridge

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"

	"go.hasen.dev/generic"
)

func (b *Bridge) ProcessPackage(pkgPath string) {

	pkgInfo, err := build.Import(pkgPath, "", build.FindOnly)
	if err != nil {
		fmt.Println("ImportDir error:", err)
		return
	}

	fset := token.NewFileSet()
	pkgs, error := parser.ParseDir(fset, pkgInfo.Dir, nil, 0)
	if error != nil {
		fmt.Println("ParseDir error:", error)
		return
	}

	var enumTypesMap = make(map[string]*EnumInfo)
	for idx := range b.Enums {
		e := &b.Enums[idx]
		enumTypesMap[e.Name] = e
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {

			// We can get the values of const names from the scope objects
			// list, but they will be out of order.
			//
			// We can get the order of declarations from the file declarations
			// list, but we won't know the values for the types
			//
			// So we grab the values first but store them for retrival for when
			// we iterate the declaration blocks (to get the declarations in
			// the declaration order)
			var nameValues = make(map[string]any)

			for _, object := range file.Scope.Objects {
				if object.Kind == ast.Con {
					nameValues[object.Name] = object.Data
				}
			}

			// process enums
			for _, decl := range file.Decls {
				genDecl, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				if genDecl.Tok != token.CONST {
					continue
				}
				var enumInfo *EnumInfo
				for _, spec := range genDecl.Specs {
					valSpec := spec.(*ast.ValueSpec)
					if typeIdent, ok := valSpec.Type.(*ast.Ident); ok {
						enumInfo = enumTypesMap[typeIdent.String()]
					}
					if enumInfo != nil {
						for _, nameIdent := range valSpec.Names {
							name := nameIdent.String()
							if name == "_" {
								continue
							}
							value := nameValues[name]
							generic.Append(&enumInfo.Consts, ConstValue{
								Name:  name,
								Value: value,
							})
						}
					}
				}
			}

			// process error decls
			for _, decl := range file.Decls {
				genDecl, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				if genDecl.Tok != token.VAR {
					continue
				}
				if len(genDecl.Specs) != 1 {
					continue
				}
				valSpec, ok := (genDecl.Specs[0]).(*ast.ValueSpec)
				if !ok {
					continue
				}
				if len(valSpec.Names) != 1 {
					continue
				}
				varName := valSpec.Names[0].Name

				// require name to start with uppercase
				if varName[0] < 'A' || varName[0] > 'Z' {
					continue
				}

				if len(valSpec.Values) != 1 {
					continue
				}
				callExpr, ok := valSpec.Values[0].(*ast.CallExpr)
				if !ok {
					continue
				}
				selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				fnPkg, ok := selExpr.X.(*ast.Ident)
				if !ok {
					continue
				}
				fnPkgName := fnPkg.Name
				fnName := selExpr.Sel.Name
				if fnPkgName != "errors" {
					continue
				}
				if fnName != "New" {
					continue
				}
				if len(callExpr.Args) != 1 {
					continue
				}
				arg, ok := callExpr.Args[0].(*ast.BasicLit)
				if !ok {
					continue
				}
				if arg.Kind != token.STRING {
					continue
				}
				generic.Append(&b.Errors, ErrorInfo{
					Name:  varName,
					Value: arg.Value,
				})
			}
		}
	}
}
