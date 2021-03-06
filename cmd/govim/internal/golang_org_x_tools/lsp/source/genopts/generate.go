// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command genopts generates JSON describing gopls' user options.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"reflect"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
	"github.com/govim/govim/cmd/govim/internal/golang_org_x_tools/lsp/source"
)

var (
	output = flag.String("output", "", "output file")
)

func main() {
	flag.Parse()
	if err := doMain(); err != nil {
		fmt.Fprintf(os.Stderr, "Generation failed: %v\n", err)
		os.Exit(1)
	}
}

func doMain() error {
	out := os.Stdout
	if *output != "" {
		var err error
		out, err = os.OpenFile(*output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0777)
		if err != nil {
			return err
		}
		defer out.Close()
	}

	content, err := generate()
	if err != nil {
		return err
	}
	if _, err := out.Write(content); err != nil {
		return err
	}

	return out.Close()
}

func generate() ([]byte, error) {
	pkgs, err := packages.Load(
		&packages.Config{
			Mode: packages.NeedTypes | packages.NeedSyntax | packages.NeedDeps,
		},
		"github.com/govim/govim/cmd/govim/internal/golang_org_x_tools/lsp/source",
	)
	if err != nil {
		return nil, err
	}
	pkg := pkgs[0]

	defaults := source.DefaultOptions()
	categories := map[string][]option{}
	for _, cat := range []reflect.Value{
		reflect.ValueOf(defaults.DebuggingOptions),
		reflect.ValueOf(defaults.UserOptions),
		reflect.ValueOf(defaults.ExperimentalOptions),
	} {
		opts, err := loadOptions(cat, pkg)
		if err != nil {
			return nil, err
		}
		catName := strings.TrimSuffix(cat.Type().Name(), "Options")
		categories[catName] = opts
	}

	marshaled, err := json.Marshal(categories)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(nil)
	fmt.Fprintf(buf, "// Code generated by \"github.com/govim/govim/cmd/govim/internal/golang_org_x_tools/lsp/source/genopts\"; DO NOT EDIT.\n\npackage source\n\nconst OptionsJson = %q\n", string(marshaled))
	return buf.Bytes(), nil
}

type option struct {
	Name    string
	Type    string
	Doc     string
	Default string
}

func loadOptions(category reflect.Value, pkg *packages.Package) ([]option, error) {
	// Find the type information and ast.File corresponding to the category.
	optsType := pkg.Types.Scope().Lookup(category.Type().Name())
	if optsType == nil {
		return nil, fmt.Errorf("could not find %v in scope %v", category.Type().Name(), pkg.Types.Scope())
	}

	fset := pkg.Fset
	var file *ast.File
	for _, f := range pkg.Syntax {
		if fset.Position(f.Pos()).Filename == fset.Position(optsType.Pos()).Filename {
			file = f
		}
	}
	if file == nil {
		return nil, fmt.Errorf("no file for opts type %v", optsType)
	}

	var opts []option
	optsStruct := optsType.Type().Underlying().(*types.Struct)
	for i := 0; i < optsStruct.NumFields(); i++ {
		// The types field gives us the type.
		typesField := optsStruct.Field(i)
		path, _ := astutil.PathEnclosingInterval(file, typesField.Pos(), typesField.Pos())
		if len(path) < 1 {
			return nil, fmt.Errorf("could not find AST node for field %v", typesField)
		}
		// The AST field gives us the doc.
		astField, ok := path[1].(*ast.Field)
		if !ok {
			return nil, fmt.Errorf("unexpected AST path %v", path)
		}

		// The reflect field gives us the default value.
		reflectField := category.FieldByName(typesField.Name())
		if !reflectField.IsValid() {
			return nil, fmt.Errorf("could not find reflect field for %v", typesField.Name())
		}

		// Format the default value. String values should look like strings. Other stuff should look like JSON literals.
		var defString string
		switch def := reflectField.Interface().(type) {
		case fmt.Stringer:
			defString = `"` + def.String() + `"`
		case string:
			defString = `"` + def + `"`
		default:
			defString = fmt.Sprint(def)
		}
		if reflectField.Kind() == reflect.Map {
			b, err := json.Marshal(reflectField.Interface())
			if err != nil {
				return nil, err
			}
			defString = string(b)
		}

		opts = append(opts, option{
			Name:    lowerFirst(typesField.Name()),
			Type:    typesField.Type().String(),
			Doc:     lowerFirst(astField.Doc.Text()),
			Default: defString,
		})
	}
	return opts, nil
}

func lowerFirst(x string) string {
	if x == "" {
		return x
	}
	return strings.ToLower(x[:1]) + x[1:]
}
