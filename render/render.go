// Copyright 2015 The Chromium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package render

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

var builtinTypeMap = map[reflect.Kind]string{
	reflect.Bool:       "bool",
	reflect.Complex128: "complex128",
	reflect.Complex64:  "complex64",
	reflect.Float32:    "float32",
	reflect.Float64:    "float64",
	reflect.Int16:      "int16",
	reflect.Int32:      "int32",
	reflect.Int64:      "int64",
	reflect.Int8:       "int8",
	reflect.Int:        "int",
	reflect.String:     "string",
	reflect.Uint16:     "uint16",
	reflect.Uint32:     "uint32",
	reflect.Uint64:     "uint64",
	reflect.Uint8:      "uint8",
	reflect.Uint:       "uint",
	reflect.Uintptr:    "uintptr",
}

var builtinTypeSet = map[string]struct{}{}

func init() {
	for _, v := range builtinTypeMap {
		builtinTypeSet[v] = struct{}{}
	}
}

var typeOfString = reflect.TypeOf("")
var typeOfInt = reflect.TypeOf(int(1))
var typeOfUint = reflect.TypeOf(uint(1))
var typeOfFloat = reflect.TypeOf(10.1)

type options struct {
	render renderOptions
	redact redactOptions
}

type renderOptions struct {
	recursionPlaceholder string
	typeFormatters       map[string]func(interface{}) string
}
type redactOptions struct {
	active                 bool
	tag                    string
	replacementPlaceholder string
	maskingChar            []byte
	maskingLength          int
	maskingReverse         bool
}

// Render converts a structure to a string representation. Unlike the "%#v"
// format string, this resolves pointer types' contents in structs, maps, and
// slices/arrays and prints their field values.
func Render(v interface{}) string {
	m := newDefaultMarshaller()
	return m.Render(v)
}

// Redact converts a structure to a string representation. Unlike the "%#v"
// format string, this resolves pointer types' contents in structs, maps, and
// slices/arrays and prints their field values.
//
// Redact also redacts struct fields based on their tags:
//
// - `redact:"REMOVE"` will remove both the field and its value as if they did
// not exist
//
// - `redact:"REPLACE"` will replace the value of the field by the "<redacted>"
// placeholder
//
// - `redact:"MASK"` will mask by the character '#' 4 characters of the value
// if its a builtin type, or of its members values if it is a
// slice/array/map/struct.
func Redact(v interface{}) string {
	m := newDefaultMarshaller()
	return m.Redact(v)
}

// renderPointer is called to render a pointer value.
//
// This is overridable so that the test suite can have deterministic pointer
// values in its expectations.
var renderPointer = func(masker *MaskWriter, p uintptr) {
	fmt.Fprintf(masker, "0x%016x", p)
}

// traverseState is used to note and avoid recursion as struct members are being
// traversed.
//
// traverseState is allowed to be nil. Specifically, the root state is nil.
type traverseState struct {
	parent *traverseState
	ptr    uintptr
}

func (s *traverseState) forkFor(ptr uintptr) *traverseState {
	for cur := s; cur != nil; cur = cur.parent {
		if ptr == cur.ptr {
			return nil
		}
	}

	fs := &traverseState{
		parent: s,
		ptr:    ptr,
	}
	return fs
}

func (s *traverseState) render(masker *MaskWriter, ptrs int, v reflect.Value, implicit bool, opts *options) {
	if v.Kind() == reflect.Invalid {
		masker.str.WriteString("nil")
		return
	}
	vt := v.Type()

	// If a formatter is registered for this value type, call it and return
	if formatted := opts.callRegisteredTypeFormatter(masker.str, ptrs, vt, v, implicit); formatted {
		return
	}
	// If the type being rendered is a potentially recursive type (a type that
	// can contain itself as a member), we need to avoid recursion.
	//
	// If we've already seen this type before, mark that this is the case and
	// write a recursion placeholder instead of actually rendering it.
	//
	// If we haven't seen it before, fork our `seen` tracking so any higher-up
	// renderers will also render it at least once, then mark that we've seen it
	// to avoid recursing on lower layers.
	pe := uintptr(0)
	vk := vt.Kind()
	switch vk {
	case reflect.Ptr:
		// Since structs and arrays aren't pointers, they can't directly be
		// recursed, but they can contain pointers to themselves. Record their
		// pointer to avoid this.
		switch v.Elem().Kind() {
		case reflect.Struct, reflect.Array:
			pe = v.Pointer()
		}

	case reflect.Slice, reflect.Map:
		pe = v.Pointer()
	}
	if pe != 0 {
		s = s.forkFor(pe)
		if s == nil {
			masker.str.WriteRune('<')
			masker.str.WriteString(opts.render.recursionPlaceholder)
			masker.str.WriteRune('(')
			if !implicit {
				writeType(masker.str, ptrs, vt)
			}
			masker.str.WriteString(")>")
			return
		}
	}

	isAnon := func(t reflect.Type) bool {
		if t.Name() != "" {
			if _, ok := builtinTypeSet[t.Name()]; !ok {
				return false
			}
		}
		return t.Kind() != reflect.Interface
	}

	switch vk {
	case reflect.Struct:
		if !implicit {
			writeType(masker.str, ptrs, vt)
		}
		structAnon := vt.Name() == ""
		masker.str.WriteRune('{')
		for i := 0; i < vt.NumField(); i++ {
			anon := structAnon && isAnon(vt.Field(i).Type)
			if opts.redact.active && s.redactField(masker, vt, v, i, anon, opts) {
				continue
			}
			if i > 0 && (!opts.redact.active || !opts.isRemoved(vt.Field(i-1))) {
				masker.str.WriteString(", ")
			}

			if !anon {
				masker.str.WriteString(vt.Field(i).Name)
				masker.str.WriteRune(':')
			}
			s.render(masker, 0, v.Field(i), anon, opts)
		}
		masker.str.WriteRune('}')

	case reflect.Slice:
		if v.IsNil() {
			if !implicit {
				writeType(masker.str, ptrs, vt)
				masker.str.WriteString("(nil)")
			} else {
				masker.str.WriteString("nil")
			}
			return
		}
		fallthrough

	case reflect.Array:
		if !implicit {
			writeType(masker.str, ptrs, vt)
		}
		anon := vt.Name() == "" && isAnon(vt.Elem())
		masker.str.WriteString("{")
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				masker.str.WriteString(", ")
			}

			s.render(masker, 0, v.Index(i), anon, opts)
		}
		masker.str.WriteRune('}')

	case reflect.Map:
		if !implicit {
			writeType(masker.str, ptrs, vt)
		}
		if v.IsNil() {
			masker.str.WriteString("(nil)")
		} else {
			masker.str.WriteString("{")

			mkeys := v.MapKeys()
			tryAndSortMapKeys(vt, mkeys)

			kt := vt.Key()
			keyAnon := typeOfString.ConvertibleTo(kt) || typeOfInt.ConvertibleTo(kt) || typeOfUint.ConvertibleTo(kt) || typeOfFloat.ConvertibleTo(kt)
			valAnon := vt.Name() == "" && isAnon(vt.Elem())
			for i, mk := range mkeys {
				if i > 0 {
					masker.str.WriteString(", ")
				}

				s.render(masker, 0, mk, keyAnon, opts)
				masker.str.WriteString(":")
				s.render(masker, 0, v.MapIndex(mk), valAnon, opts)
			}
			masker.str.WriteRune('}')
		}

	case reflect.Ptr:
		ptrs++
		fallthrough
	case reflect.Interface:
		if v.IsNil() {
			writeType(masker.str, ptrs, v.Type())
			masker.str.WriteString("(nil)")
		} else {
			s.render(masker, ptrs, v.Elem(), false, opts)
		}

	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		writeType(masker.str, ptrs, vt)
		masker.str.WriteRune('(')
		renderPointer(masker, v.Pointer())
		masker.str.WriteRune(')')

	default:
		tstr := vt.String()
		implicit = implicit || (ptrs == 0 && builtinTypeMap[vk] == tstr)
		if !implicit {
			writeType(masker.str, ptrs, vt)
			masker.str.WriteRune('(')
		}

		switch vk {
		case reflect.String:
			value := v.String()
			if masker.masked {
				writer := NewDefaultMaskWriter(masker.masked, opts.redact)
				writer.WriteString(value)
				value = writer.String()
			}
			fmt.Fprintf(masker.str, "%q", value)
		default:
			switch vk {
			case reflect.Bool:
				fmt.Fprintf(masker, "%v", v.Bool())
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				fmt.Fprintf(masker, "%d", v.Int())

			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
				fmt.Fprintf(masker, "%d", v.Uint())

			case reflect.Float32, reflect.Float64:
				fmt.Fprintf(masker, "%g", v.Float())
			case reflect.Complex64, reflect.Complex128:
				fmt.Fprintf(masker, "%g", v.Complex())
			}
		}

		if !implicit {
			masker.str.WriteRune(')')
		}
	}
}

func writeType(str *strings.Builder, ptrs int, t reflect.Type) {
	parens := ptrs > 0
	switch t.Kind() {
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		parens = true
	}

	if parens {
		str.WriteRune('(')
		for i := 0; i < ptrs; i++ {
			str.WriteRune('*')
		}
	}

	switch t.Kind() {
	case reflect.Ptr:
		if ptrs == 0 {
			// This pointer was referenced from within writeType (e.g., as part of
			// rendering a list), and so hasn't had its pointer asterisk accounted
			// for.
			str.WriteRune('*')
		}
		writeType(str, 0, t.Elem())

	case reflect.Interface:
		if n := t.Name(); n != "" {
			str.WriteString(t.String())
		} else {
			str.WriteString("interface{}")
		}

	case reflect.Array:
		str.WriteRune('[')
		str.WriteString(strconv.FormatInt(int64(t.Len()), 10))
		str.WriteRune(']')
		writeType(str, 0, t.Elem())

	case reflect.Slice:
		if t == reflect.SliceOf(t.Elem()) {
			str.WriteString("[]")
			writeType(str, 0, t.Elem())
		} else {
			// Custom slice type, use type name.
			str.WriteString(t.String())
		}

	case reflect.Map:
		if t == reflect.MapOf(t.Key(), t.Elem()) {
			str.WriteString("map[")
			writeType(str, 0, t.Key())
			str.WriteRune(']')
			writeType(str, 0, t.Elem())
		} else {
			// Custom map type, use type name.
			str.WriteString(t.String())
		}

	default:
		str.WriteString(t.String())
	}

	if parens {
		str.WriteRune(')')
	}
}

type cmpFn func(a, b reflect.Value) int

type sortableValueSlice struct {
	cmp      cmpFn
	elements []reflect.Value
}

func (s sortableValueSlice) Len() int {
	return len(s.elements)
}

func (s sortableValueSlice) Less(i, j int) bool {
	return s.cmp(s.elements[i], s.elements[j]) < 0
}

func (s sortableValueSlice) Swap(i, j int) {
	s.elements[i], s.elements[j] = s.elements[j], s.elements[i]
}

// cmpForType returns a cmpFn which sorts the data for some type t in the same
// order that a go-native map key is compared for equality.
func cmpForType(t reflect.Type) cmpFn {
	switch t.Kind() {
	case reflect.String:
		return func(av, bv reflect.Value) int {
			a, b := av.String(), bv.String()
			if a < b {
				return -1
			} else if a > b {
				return 1
			}
			return 0
		}

	case reflect.Bool:
		return func(av, bv reflect.Value) int {
			a, b := av.Bool(), bv.Bool()
			if !a && b {
				return -1
			} else if a && !b {
				return 1
			}
			return 0
		}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return func(av, bv reflect.Value) int {
			a, b := av.Int(), bv.Int()
			if a < b {
				return -1
			} else if a > b {
				return 1
			}
			return 0
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr, reflect.UnsafePointer:
		return func(av, bv reflect.Value) int {
			a, b := av.Uint(), bv.Uint()
			if a < b {
				return -1
			} else if a > b {
				return 1
			}
			return 0
		}

	case reflect.Float32, reflect.Float64:
		return func(av, bv reflect.Value) int {
			a, b := av.Float(), bv.Float()
			if a < b {
				return -1
			} else if a > b {
				return 1
			}
			return 0
		}

	case reflect.Interface:
		return func(av, bv reflect.Value) int {
			a, b := av.InterfaceData(), bv.InterfaceData()
			if a[0] < b[0] {
				return -1
			} else if a[0] > b[0] {
				return 1
			}
			if a[1] < b[1] {
				return -1
			} else if a[1] > b[1] {
				return 1
			}
			return 0
		}

	case reflect.Complex64, reflect.Complex128:
		return func(av, bv reflect.Value) int {
			a, b := av.Complex(), bv.Complex()
			if real(a) < real(b) {
				return -1
			} else if real(a) > real(b) {
				return 1
			}
			if imag(a) < imag(b) {
				return -1
			} else if imag(a) > imag(b) {
				return 1
			}
			return 0
		}

	case reflect.Ptr, reflect.Chan:
		return func(av, bv reflect.Value) int {
			a, b := av.Pointer(), bv.Pointer()
			if a < b {
				return -1
			} else if a > b {
				return 1
			}
			return 0
		}

	case reflect.Struct:
		cmpLst := make([]cmpFn, t.NumField())
		for i := range cmpLst {
			cmpLst[i] = cmpForType(t.Field(i).Type)
		}
		return func(a, b reflect.Value) int {
			for i, cmp := range cmpLst {
				if rslt := cmp(a.Field(i), b.Field(i)); rslt != 0 {
					return rslt
				}
			}
			return 0
		}
	}

	return nil
}

func tryAndSortMapKeys(mt reflect.Type, k []reflect.Value) {
	if cmp := cmpForType(mt.Key()); cmp != nil {
		sort.Sort(sortableValueSlice{cmp, k})
	}
}

func (s *traverseState) redactField(masker *MaskWriter, vt reflect.Type, v reflect.Value, i int, anon bool, opts *options) bool {
	tag, ok := vt.Field(i).Tag.Lookup(opts.redact.tag)
	if !ok {
		return false
	}
	switch tag {
	case REMOVE:
		// no field, no value
		return true
	default:
		// write field
		if i > 0 && !opts.isRemoved(vt.Field(i-1)) {
			masker.str.WriteString(", ")
		}
		if !anon {
			masker.str.WriteString(vt.Field(i).Name)
			masker.str.WriteRune(':')
		}
		switch {
		case tag == REPLACE:
			masker.str.WriteRune('<')
			masker.str.WriteString(opts.redact.replacementPlaceholder)
			masker.str.WriteRune('>')
			return true
		case tag == MASK || masker.masked:
			maskedMasker := NewMaskWriter(masker.str, true, opts.redact)
			s.render(maskedMasker, 0, v.Field(i), anon, opts)
			return true
		}
	}
	return false
}

func (o *options) isRemoved(vt reflect.StructField) bool {
	tag, ok := vt.Tag.Lookup(o.redact.tag)
	if !ok {
		return false
	}
	if tag == REMOVE {
		return true
	}
	return false
}

func (o *options) callRegisteredTypeFormatter(str *strings.Builder, ptrs int, vt reflect.Type, v reflect.Value, implicit bool) (formatted bool) {
	if typeFormatter, ok := o.render.typeFormatters[vt.String()]; ok {
		fmt.Println(o.render.typeFormatters, vt.String())
		// register a recover to avoid panicking on user provided type formatter
		defer func() {
			if panicError := recover(); panicError != nil {
				fmt.Println("PANIC?", panicError)
				formatted = false
			}
		}()
		value, ok := getInterface(v)
		if !ok {
			return false
		}
		formattedType := typeFormatter(value)
		if !implicit {
			writeType(str, ptrs, vt)
		}
		str.WriteRune('(')
		str.WriteString(formattedType)
		str.WriteRune(')')
		return true
	}
	return false
}

func getInterface(v reflect.Value) (interface{}, bool) {
	if v.CanInterface() {
		return v.Interface(), true
	}
	switch v.Kind() {
	case reflect.Invalid:
		return nil, false
	case reflect.Bool:
		return v.Bool(), true
	case reflect.Int:
		return int(v.Int()), true
	case reflect.Int8:
		return int8(v.Int()), true
	case reflect.Int16:
		return int16(v.Int()), true
	case reflect.Int32:
		return int32(v.Int()), true
	case reflect.Int64:
		return v.Int(), true
	case reflect.Uint:
		return uint(v.Uint()), true
	case reflect.Uint8:
		return uint8(v.Uint()), true
	case reflect.Uint16:
		return uint16(v.Uint()), true
	case reflect.Uint32:
		return uint32(v.Uint()), true
	case reflect.Uint64:
		return v.Uint(), true
	case reflect.Uintptr:
		return uintptr(v.Uint()), true
	case reflect.Float32:
		return float32(v.Float()), true
	case reflect.Float64:
		return v.Float(), true
	case reflect.Complex64:
		return complex64(v.Complex()), true
	case reflect.Complex128:
		return v.Complex(), true
	case reflect.Array, reflect.Slice:
		var copy reflect.Value
		reflect.Copy(copy, v)
		return copy.Interface(), true
	case reflect.Chan, reflect.Func:
		return v.Pointer(), true
	case reflect.Map:
		return v.Pointer(), true
	case reflect.Ptr:
		return v.Pointer(), true
		// reflect.Chan
		// reflect.Func
		// reflect.Interface
		// reflect.Map
		// reflect.Ptr
		// reflect.Slice
	case reflect.String:
		return v.String(), true
	case reflect.Struct:
		s := reflect.New(v.Type())
		for i := 0; i < v.NumField(); i++ {
			fieldValue, ok := getInterface(v.Field(i))
			if !ok {
				return nil, false
			}
			s.Elem().Field(i).Set(reflect.ValueOf(fieldValue))
		}
		return s.Elem().Interface(), true
	case reflect.UnsafePointer:
		return v.UnsafeAddr(), true
	default:
		return nil, true
	}
}
