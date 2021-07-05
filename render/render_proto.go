package render

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"github.com/reno-xjb/go-render-redact/render/protocol/redact"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// RenderProto converts a structure to a string representation. Unlike the "%#v"
// format string, this resolves pointer types' contents in structs, maps, and
// slices/arrays and prints their field values.
func RenderProto(v proto.Message) string {
	m := newDefaultMarshaller()
	return m.RenderProto(v)
}

// RedactProto converts a structure to a string representation. Unlike the "%#v"
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
//
// If your message is V1 use func MessageV2(m GeneratedMessage) protoV2.Message (github.com/golang/protobuf)
func RedactProto(v proto.Message) string {
	m := newDefaultMarshaller()
	return m.RedactProto(v)
}

func (s *traverseState) renderProtoMessage(str *strings.Builder, msg protoreflect.Message, mask bool, opts *options) {
	if !msg.IsValid() {
		str.WriteString("nil")
		return
	}

	desc := msg.Descriptor()

	v := reflect.ValueOf(msg.Interface())
	pe := v.Pointer()
	s = s.forkFor(pe)
	if s == nil {
		str.WriteRune('<')
		str.WriteString(opts.render.recursionPlaceholder)
		str.WriteRune('(')
		str.WriteString("*proto.Message(")
		str.WriteString(string(desc.FullName()))
		str.WriteRune(')')
		str.WriteString(")>")
		return
	}
	str.WriteString("*proto.Message(")
	str.WriteString(string(desc.FullName()))

	str.WriteRune('{')
	fields := desc.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		value := msg.Get(field)
		if opts.redact.active && s.redactProtoMessageField(str, msg, i, mask, opts) {
			fmt.Printf("WTF: active %v\n", opts.redact.active)
			continue
		}
		// if i > 0 && (!opts.redact.active || !opts.isRemoved(vt.Field(i-1))) {
		if i > 0 {
			str.WriteString(", ")
		}

		str.WriteString(string(field.Name()))
		str.WriteRune(':')
		s.renderProtoMessageField(str, field, value, mask, opts)
	}
	str.WriteRune('}')
	str.WriteRune(')')
}

func (s *traverseState) renderProtoMessageField(str *strings.Builder, field protoreflect.FieldDescriptor, value protoreflect.Value, mask bool, opts *options) {
	maskWriter := NewMaskWriter(str, mask, opts.redact)
	switch field.Kind() {
	case protoreflect.EnumKind:
		enum := field.Enum()
		enumName := enum.Values().ByNumber(value.Enum()).FullName()
		fmt.Fprintf(maskWriter, "%s(%s)", enum.FullName(), enumName)
	case protoreflect.MessageKind, protoreflect.GroupKind:
		s.renderProtoMessage(str, value.Message(), mask, opts)
	case protoreflect.BoolKind:
		fmt.Fprintf(maskWriter, "%t", value.Bool())
	case protoreflect.StringKind:
		fmt.Fprintf(maskWriter, "%q", value.String())
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind, protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		fmt.Fprintf(maskWriter, "%d", value.Int())
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		fmt.Fprintf(maskWriter, "%d", value.Uint())
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		fmt.Fprintf(maskWriter, "%g", value.Float())
	case protoreflect.BytesKind:
		fmt.Fprintf(maskWriter, "%q", value.Bytes())
	}
}

type MaskWriter struct {
	str     *strings.Builder
	masked  bool
	options redactOptions
}

func NewDefaultMaskWriter(masked bool, options redactOptions) *MaskWriter {
	str := strings.Builder{}
	return NewMaskWriter(&str, masked, options)
}

func NewMaskWriter(str *strings.Builder, masked bool, options redactOptions) *MaskWriter {
	return &MaskWriter{
		str,
		masked,
		options,
	}
}

func (w *MaskWriter) EnableMask() {
	w.masked = true
}

func (w *MaskWriter) DisableMask() {
	w.masked = false
}

func (w *MaskWriter) String() string {
	return w.str.String()
}

func (w *MaskWriter) Write(p []byte) (n int, err error) {
	if !w.options.active || !w.masked {
		return w.str.Write(p)
	}
	// whole string
	if w.options.maskingLength < 0 || w.options.maskingLength >= len(p) {
		return w.str.Write(bytes.Repeat(w.options.maskingChar, len(p)))
	}
	// reverse
	if w.options.maskingReverse {
		n, err = w.str.Write(p[:len(p)-w.options.maskingLength])
		if err != nil {
			return n, err
		}
		m, err := w.str.Write(bytes.Repeat(w.options.maskingChar, w.options.maskingLength))
		return m + n, err
	}
	// straight
	n, err = w.str.Write(bytes.Repeat(w.options.maskingChar, w.options.maskingLength))
	if err != nil {
		return n, err
	}
	m, err := w.str.Write(p[w.options.maskingLength:])
	return m + n, err
}

func (w *MaskWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// func writeType(str *strings.Builder, ptrs int, t reflect.Type) {
// 	parens := ptrs > 0
// 	switch t.Kind() {
// 	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
// 		parens = true
// 	}
//
// 	if parens {
// 		str.WriteRune('(')
// 		for i := 0; i < ptrs; i++ {
// 			str.WriteRune('*')
// 		}
// 	}
//
// 	switch t.Kind() {
// 	case reflect.Ptr:
// 		if ptrs == 0 {
// 			// This pointer was referenced from within writeType (e.g., as part of
// 			// rendering a list), and so hasn't had its pointer asterisk accounted
// 			// for.
// 			str.WriteRune('*')
// 		}
// 		writeType(str, 0, t.Elem())
//
// 	case reflect.Interface:
// 		if n := t.Name(); n != "" {
// 			str.WriteString(t.String())
// 		} else {
// 			str.WriteString("interface{}")
// 		}
//
// 	case reflect.Array:
// 		str.WriteRune('[')
// 		str.WriteString(strconv.FormatInt(int64(t.Len()), 10))
// 		str.WriteRune(']')
// 		writeType(str, 0, t.Elem())
//
// 	case reflect.Slice:
// 		if t == reflect.SliceOf(t.Elem()) {
// 			str.WriteString("[]")
// 			writeType(str, 0, t.Elem())
// 		} else {
// 			// Custom slice type, use type name.
// 			str.WriteString(t.String())
// 		}
//
// 	case reflect.Map:
// 		if t == reflect.MapOf(t.Key(), t.Elem()) {
// 			str.WriteString("map[")
// 			writeType(str, 0, t.Key())
// 			str.WriteRune(']')
// 			writeType(str, 0, t.Elem())
// 		} else {
// 			// Custom map type, use type name.
// 			str.WriteString(t.String())
// 		}
//
// 	default:
// 		str.WriteString(t.String())
// 	}
//
// 	if parens {
// 		str.WriteRune(')')
// 	}
// }
//
// type cmpFn func(a, b reflect.Value) int
//
// type sortableValueSlice struct {
// 	cmp      cmpFn
// 	elements []reflect.Value
// }
//
// func (s sortableValueSlice) Len() int {
// 	return len(s.elements)
// }
//
// func (s sortableValueSlice) Less(i, j int) bool {
// 	return s.cmp(s.elements[i], s.elements[j]) < 0
// }
//
// func (s sortableValueSlice) Swap(i, j int) {
// 	s.elements[i], s.elements[j] = s.elements[j], s.elements[i]
// }
//
// // cmpForType returns a cmpFn which sorts the data for some type t in the same
// // order that a go-native map key is compared for equality.
// func cmpForType(t reflect.Type) cmpFn {
// 	switch t.Kind() {
// 	case reflect.String:
// 		return func(av, bv reflect.Value) int {
// 			a, b := av.String(), bv.String()
// 			if a < b {
// 				return -1
// 			} else if a > b {
// 				return 1
// 			}
// 			return 0
// 		}
//
// 	case reflect.Bool:
// 		return func(av, bv reflect.Value) int {
// 			a, b := av.Bool(), bv.Bool()
// 			if !a && b {
// 				return -1
// 			} else if a && !b {
// 				return 1
// 			}
// 			return 0
// 		}
//
// 	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
// 		return func(av, bv reflect.Value) int {
// 			a, b := av.Int(), bv.Int()
// 			if a < b {
// 				return -1
// 			} else if a > b {
// 				return 1
// 			}
// 			return 0
// 		}
//
// 	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
// 		reflect.Uint64, reflect.Uintptr, reflect.UnsafePointer:
// 		return func(av, bv reflect.Value) int {
// 			a, b := av.Uint(), bv.Uint()
// 			if a < b {
// 				return -1
// 			} else if a > b {
// 				return 1
// 			}
// 			return 0
// 		}
//
// 	case reflect.Float32, reflect.Float64:
// 		return func(av, bv reflect.Value) int {
// 			a, b := av.Float(), bv.Float()
// 			if a < b {
// 				return -1
// 			} else if a > b {
// 				return 1
// 			}
// 			return 0
// 		}
//
// 	case reflect.Interface:
// 		return func(av, bv reflect.Value) int {
// 			a, b := av.InterfaceData(), bv.InterfaceData()
// 			if a[0] < b[0] {
// 				return -1
// 			} else if a[0] > b[0] {
// 				return 1
// 			}
// 			if a[1] < b[1] {
// 				return -1
// 			} else if a[1] > b[1] {
// 				return 1
// 			}
// 			return 0
// 		}
//
// 	case reflect.Complex64, reflect.Complex128:
// 		return func(av, bv reflect.Value) int {
// 			a, b := av.Complex(), bv.Complex()
// 			if real(a) < real(b) {
// 				return -1
// 			} else if real(a) > real(b) {
// 				return 1
// 			}
// 			if imag(a) < imag(b) {
// 				return -1
// 			} else if imag(a) > imag(b) {
// 				return 1
// 			}
// 			return 0
// 		}
//
// 	case reflect.Ptr, reflect.Chan:
// 		return func(av, bv reflect.Value) int {
// 			a, b := av.Pointer(), bv.Pointer()
// 			if a < b {
// 				return -1
// 			} else if a > b {
// 				return 1
// 			}
// 			return 0
// 		}
//
// 	case reflect.Struct:
// 		cmpLst := make([]cmpFn, t.NumField())
// 		for i := range cmpLst {
// 			cmpLst[i] = cmpForType(t.Field(i).Type)
// 		}
// 		return func(a, b reflect.Value) int {
// 			for i, cmp := range cmpLst {
// 				if rslt := cmp(a.Field(i), b.Field(i)); rslt != 0 {
// 					return rslt
// 				}
// 			}
// 			return 0
// 		}
// 	}
//
// 	return nil
// }

// func tryAndSortMapKeys(mt reflect.Type, k []reflect.Value) {
// 	if cmp := cmpForType(mt.Key()); cmp != nil {
// 		sort.Sort(sortableValueSlice{cmp, k})
// 	}
// }
//
func (s *traverseState) redactProtoMessageField(str *strings.Builder, msg protoreflect.Message, i int, mask bool, opts *options) bool {
	field := msg.Descriptor().Fields().Get(i)
	extension := proto.GetExtension(field.Options(), redact.E_Mode)
	if extension == nil {
		return false
	}
	mode := extension.(redact.Mode)
	switch mode {
	case redact.Mode_REMOVE:
		// no field, no value
		return true
	default:
		// write field
		if i > 0 && !opts.isProtoMessageFieldRemoved(msg.Descriptor().Fields().Get(i-1)) {
			str.WriteString(", ")
		}
		str.WriteString(string(field.Name()))
		str.WriteRune(':')
		switch {
		case mode == redact.Mode_REPLACE:
			str.WriteRune('<')
			str.WriteString(opts.redact.replacementPlaceholder)
			str.WriteRune('>')
			return true
		case mode == redact.Mode_MASK || mask:
			s.renderProtoMessageField(str, field, msg.Get(field), true, opts)
			return true
		}
	}
	return false
}

// func (o *options) mask(str *strings.Builder, value string) {
// 	if !o.redact.active {
// 		str.WriteString(value)
// 		return
// 	}
// 	// whole string
// 	if o.redact.maskingLength < 0 || o.redact.maskingLength >= len(value) {
// 		str.WriteString(strings.Repeat(string(o.redact.maskingChar), len(value)))
// 		return
// 	}
// 	// reverse
// 	if o.redact.maskingReverse {
// 		str.WriteString(value[:len(value)-o.redact.maskingLength])
// 		str.WriteString(strings.Repeat(string(o.redact.maskingChar), o.redact.maskingLength))
// 		return
// 	}
// 	// straight
// 	str.WriteString(strings.Repeat(string(o.redact.maskingChar), o.redact.maskingLength))
// 	str.WriteString(value[o.redact.maskingLength:])
// }
//
func (o *options) isProtoMessageFieldRemoved(field protoreflect.FieldDescriptor) bool {
	extension := proto.GetExtension(field.Options(), redact.E_Mode)
	if extension == nil {
		return false
	}
	mode := extension.(*redact.Mode)
	switch *mode {
	case redact.Mode_REMOVE:
		return true
	}
	return false
}

//
// func (o *options) callRegisteredTypeFormatter(str *strings.Builder, ptrs int, vt reflect.Type, v reflect.Value, implicit bool) (formatted bool) {
// 	if typeFormatter, ok := o.render.typeFormatters[vt.String()]; ok {
// 		// register a recover to avoid panicking on user provided type formatter
// 		defer func() {
// 			if panicError := recover(); panicError != nil {
// 				formatted = false
// 			}
// 		}()
// 		formattedType := typeFormatter(v.Interface())
// 		if !implicit {
// 			writeType(str, ptrs, vt)
// 		}
// 		str.WriteRune('(')
// 		str.WriteString(formattedType)
// 		str.WriteRune(')')
// 		return true
// 	}
// 	return false
// }
