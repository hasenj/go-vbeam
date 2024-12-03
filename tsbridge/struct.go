package tsbridge

// Supporting structs in typescript

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

type Bridge struct {
	Structs []StructInfo
	Enums   []EnumInfo
	Errors  []ErrorInfo

	// types we want to process (structs/enums)
	ProcessedTypes []reflect.Type
	QueuedTypes    []reflect.Type

	// packages we want to process consts for
	QueuedPackages    []string
	ProcessedPackages []string
}

type StructInfo struct {
	Name   string
	Fields []StructField
}

type StructField struct {
	Name       string
	TypeName   string
	TypeCustom bool
}

type EnumInfo struct {
	Name     string
	TypeName string
	Consts   []ConstValue
}

type ErrorInfo struct {
	Name  string
	Value string // go-quoted string
}

type ConstValue struct {
	Name  string
	Value any // int or string
}

func (b *Bridge) QueueObject(x interface{}) {
	b.QueueType(reflect.TypeOf(x))
}

func (b *Bridge) QueueType(t reflect.Type) {
	// don't care about native types
	if t.Name() == t.Kind().String() {
		return
	}

	// make sure we don't already have it!
	for _, pt := range b.QueuedTypes {
		if t == pt {
			return
		}
	}
	for _, pt := range b.ProcessedTypes {
		if t == pt {
			return
		}
	}
	// fmt.Println("Adding to queue:", t)
	b.QueuedTypes = append(b.QueuedTypes, t)
}

func (b *Bridge) QueuePackage(pkgPath string) {
	for _, pp := range b.QueuedPackages {
		if pp == pkgPath {
			return
		}
	}
	for _, pp := range b.ProcessedPackages {
		if pp == pkgPath {
			return
		}
	}
	b.QueuedPackages = append(b.QueuedPackages, pkgPath)
}

func (b *Bridge) Process() {
	for len(b.QueuedTypes) > 0 {
		var t = b.QueuedTypes[0]
		b.processType(t)
		b.QueuedTypes = b.QueuedTypes[1:]
	}

	for _, pkgPath := range b.QueuedPackages {
		b.ProcessPackage(pkgPath)
	}
}

func (b *Bridge) processType(t reflect.Type) {
	// fmt.Println("Processing: ", t)
	var kind = t.Kind()
	if kind == reflect.Struct {
		var sinfo StructInfo
		sinfo.Name = t.Name()
		b.AddStructFields(&sinfo, t)
		b.Structs = append(b.Structs, sinfo)
		b.ProcessedTypes = append(b.ProcessedTypes, t)
	} else {
		b.QueuePackage(t.PkgPath())
		var einfo EnumInfo
		einfo.Name = t.Name()
		einfo.TypeName = DecideEnumTypeName(t)
		b.Enums = append(b.Enums, einfo)
		b.ProcessedTypes = append(b.ProcessedTypes, t)
	}
}

func (b *Bridge) AddStructFields(sinfo *StructInfo, t reflect.Type) {
	var numFields = t.NumField()
	for index := 0; index < numFields; index++ {
		var field = t.Field(index)
		if field.Anonymous {
			b.QueueType(field.Type)
			b.AddStructFields(sinfo, field.Type)
			continue
		}
		var sField StructField // our data
		sField.Name = field.Name
		sField.TypeName = field.Type.Name()
		var jsonTag = field.Tag.Get("json")
		if jsonTag != "" {
			var parts = strings.Split(jsonTag, ",")
			if parts[0] != "" {
				if parts[0] == "-" {
					continue
				}
				sField.Name = parts[0]
			}
		}
		var tsTag = field.Tag.Get("ts")
		if tsTag != "" {
			var parts = strings.Split(tsTag, ",")
			if parts[0] != "" {
				sField.TypeName = parts[0]
				sField.TypeCustom = true
			}
		}
		if !sField.TypeCustom {
			sField.TypeName = b.DecideTypescriptTypeName(field.Type)
		}
		sinfo.Fields = append(sinfo.Fields, sField)
	}
}

func DecideEnumTypeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "number"
	case reflect.String:
		return "string"
	default:
		return "void"
	}
}

func (b *Bridge) DecideTypescriptTypeName(t reflect.Type) string {
	var kind = t.Kind()
	// fmt.Println("Type:", t, "Kind:", kind)

	switch kind {
	case reflect.Struct:
		b.QueueType(t)
		return t.Name()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		if t.String() != kind.String() {
			// potentially an enum!
			// fmt.Println("  Maybe an enum!")
			b.QueueType(t)
			return t.Name()
		}
		return "number"
	case reflect.String:
		if t.String() != kind.String() {
			// potentially an enum!
			// fmt.Println("  Maybe an enum!")
			b.QueueType(t)
			return t.Name()
		}
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice, reflect.Array:
		var elementType = b.DecideTypescriptTypeName(t.Elem())
		if elementType == "" {
			return ""
		}
		return elementType + "[]"
	case reflect.Map:
		var elementType = b.DecideTypescriptTypeName(t.Elem())
		if elementType == "" {
			return ""
		}
		var keyType = b.DecideTypescriptTypeName(t.Key())
		if keyType == "" {
			return ""
		}
		return fmt.Sprintf("Record<%s, %s>", keyType, elementType)
	case reflect.Ptr:
		var elementType = b.DecideTypescriptTypeName(t.Elem())
		if elementType == "" {
			return ""
		}
		return elementType + " | null"
	case reflect.Interface:
		// TODO: should we create a matching TS interface type?!
		return "any"
	default:
		return "unknown"
	}
}

func WriteStructTSBinding(b *Bridge, w io.Writer) {
	for index := range b.Enums {
		var einfo = &b.Enums[index]
		fmt.Fprintf(w, "export type %s = %s;\n", einfo.Name, einfo.TypeName)
		for _, c := range einfo.Consts {
			b, err := json.Marshal(c.Value)
			if err != nil {
				fmt.Printf("Could not print out the constant value for %s: got error: %v\n", c.Name, err)
			}
			fmt.Fprintf(w, "export const %s: %s = %s;\n", c.Name, einfo.Name, b)
		}
		fmt.Fprintln(w)
	}

	if len(b.Errors) > 0 {
		fmt.Fprintln(w, "// Errors")
		for index := range b.Errors {
			var einfo = &b.Errors[index]
			fmt.Fprintf(w, "export const %s = %s;\n", einfo.Name, einfo.Value)
		}
		fmt.Fprintln(w)
	}

	for index := range b.Structs {
		var sinfo = &b.Structs[index]
		fmt.Fprintf(w, "export interface %s {\n", sinfo.Name)
		for findex := range sinfo.Fields {
			var field = &sinfo.Fields[findex]
			fmt.Fprintf(w, "    %s: %s\n", field.Name, field.TypeName)
		}
		fmt.Fprintf(w, "}\n\n")
	}
}
