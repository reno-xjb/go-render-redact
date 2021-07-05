// Copyright 2015 The Chromium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package render

import (
	"fmt"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func init() {
	// For testing purposes, pointers will render as "PTR" so that they are
	// deterministic.
	renderPointer = func(masker *MaskWriter, p uintptr) {
		masker.WriteString("PTR")
	}
}

func assertRendersLike(t *testing.T, name string, v interface{}, exp string) {
	act := Render(v)
	if act != exp {
		_, _, line, _ := runtime.Caller(1)
		t.Errorf("On line #%d, [%s] did not match expectations:\nExpected: %s\nActual  : %s\n", line, name, exp, act)
	}
}

func TestRenderList(t *testing.T) {
	t.Parallel()

	// Note that we make some of the fields exportable. This is to avoid a fun case
	// where the first reflect.Value has a read-only bit set, but follow-on values
	// do not, so recursion tests are off by one.
	// "redact" tag should have no impact here
	type testStruct struct {
		Name string      `redact:"REMOVE"`
		I    interface{} `redact:"REPLACE"`

		m string `redact:"MASK"`
	}

	type myStringSlice []string
	type myStringMap map[string]string
	type myIntType int
	type myStringType string

	s0 := "string0"
	s0P := &s0
	mit := myIntType(42)
	stringer := fmt.Stringer(nil)

	for i, tc := range []struct {
		a interface{}
		s string
	}{
		{nil, `nil`},
		{make(chan int), `(chan int)(PTR)`},
		{&stringer, `(*fmt.Stringer)(nil)`},
		{123, `123`},
		{"hello", `"hello"`},
		{(*testStruct)(nil), `(*render.testStruct)(nil)`},
		{(**testStruct)(nil), `(**render.testStruct)(nil)`},
		{[]***testStruct(nil), `[]***render.testStruct(nil)`},
		{testStruct{Name: "foo", I: &testStruct{Name: "baz"}},
			`render.testStruct{Name:"foo", I:(*render.testStruct){Name:"baz", I:interface{}(nil), m:""}, m:""}`},
		{[]byte(nil), `[]uint8(nil)`},
		{[]byte{}, `[]uint8{}`},
		{map[string]string(nil), `map[string]string(nil)`},
		{[]*testStruct{
			{Name: "foo"},
			{Name: "bar"},
		}, `[]*render.testStruct{(*render.testStruct){Name:"foo", I:interface{}(nil), m:""}, ` +
			`(*render.testStruct){Name:"bar", I:interface{}(nil), m:""}}`},
		{myStringSlice{"foo", "bar"}, `render.myStringSlice{"foo", "bar"}`},
		{myStringMap{"foo": "bar"}, `render.myStringMap{"foo":"bar"}`},
		{myIntType(12), `render.myIntType(12)`},
		{&mit, `(*render.myIntType)(42)`},
		{myStringType("foo"), `render.myStringType("foo")`},
		{struct {
			a int
			b string
		}{123, "foo"}, `struct { a int; b string }{123, "foo"}`},
		{[]string{"foo", "foo", "bar", "baz", "qux", "qux"},
			`[]string{"foo", "foo", "bar", "baz", "qux", "qux"}`},
		{[...]int{1, 2, 3}, `[3]int{1, 2, 3}`},
		{map[string]bool{
			"foo": true,
			"bar": false,
		}, `map[string]bool{"bar":false, "foo":true}`},
		{map[int]string{1: "foo", 2: "bar"}, `map[int]string{1:"foo", 2:"bar"}`},
		{uint32(1337), `1337`},
		{3.14, `3.14`},
		{complex(3, 0.14), `(3+0.14i)`},
		{&s0, `(*string)("string0")`},
		{&s0P, `(**string)("string0")`},
		{[]interface{}{nil, 1, 2, nil}, `[]interface{}{interface{}(nil), 1, 2, interface{}(nil)}`},
	} {
		assertRendersLike(t, fmt.Sprintf("Input #%d", i), tc.a, tc.s)
	}
}

func TestRenderRecursiveStruct(t *testing.T) {
	type testStruct struct {
		Name string
		I    interface{}
	}

	s := &testStruct{
		Name: "recursive",
	}
	s.I = s

	assertRendersLike(t, "Recursive struct", s,
		`(*render.testStruct){Name:"recursive", I:<recursive(*render.testStruct)>}`)
}

func TestRenderRecursiveArray(t *testing.T) {
	a := [2]interface{}{}
	a[0] = &a
	a[1] = &a

	assertRendersLike(t, "Recursive array", &a,
		`(*[2]interface{}){<recursive(*[2]interface{})>, <recursive(*[2]interface{})>}`)
}

func TestRenderRecursiveMap(t *testing.T) {
	m := map[string]interface{}{}
	foo := "foo"
	m["foo"] = m
	m["bar"] = [](*string){&foo, &foo}
	v := []map[string]interface{}{m, m}

	assertRendersLike(t, "Recursive map", v,
		`[]map[string]interface{}{{`+
			`"bar":[]*string{(*string)("foo"), (*string)("foo")}, `+
			`"foo":<recursive(map[string]interface{})>}, {`+
			`"bar":[]*string{(*string)("foo"), (*string)("foo")}, `+
			`"foo":<recursive(map[string]interface{})>}}`)
}

func TestRenderImplicitType(t *testing.T) {
	type namedStruct struct{ a, b int }
	type namedInt int

	tcs := []struct {
		in     interface{}
		expect string
	}{
		{
			[]struct{ a, b int }{{1, 2}},
			"[]struct { a int; b int }{{1, 2}}",
		},
		{
			map[string]struct{ a, b int }{"hi": {1, 2}},
			`map[string]struct { a int; b int }{"hi":{1, 2}}`,
		},
		{
			map[namedInt]struct{}{10: {}},
			`map[render.namedInt]struct {}{10:{}}`,
		},
		{
			struct{ a, b int }{1, 2},
			`struct { a int; b int }{1, 2}`,
		},
		{
			namedStruct{1, 2},
			"render.namedStruct{a:1, b:2}",
		},
	}

	for _, tc := range tcs {
		assertRendersLike(t, reflect.TypeOf(tc.in).String(), tc.in, tc.expect)
	}
}

func ExampleInReadme() {
	type customType int
	type testStruct struct {
		S string
		V *map[string]int
		I interface{}
	}

	a := testStruct{
		S: "hello",
		V: &map[string]int{"foo": 0, "bar": 1},
		I: customType(42),
	}

	fmt.Println("Render test:")
	fmt.Printf("fmt.Printf:    %s\n", sanitizePointer(fmt.Sprintf("%#v", a)))
	fmt.Printf("render.Render: %s\n", Render(a))
	// Output: Render test:
	// fmt.Printf:    render.testStruct{S:"hello", V:(*map[string]int)(0x600dd065), I:42}
	// render.Render: render.testStruct{S:"hello", V:(*map[string]int){"bar":1, "foo":0}, I:render.customType(42)}
}

var pointerRE = regexp.MustCompile(`\(0x[a-f0-9]+\)`)

func sanitizePointer(s string) string {
	return pointerRE.ReplaceAllString(s, "(0x600dd065)")
}

type chanList []chan int

func (c chanList) Len() int      { return len(c) }
func (c chanList) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c chanList) Less(i, j int) bool {
	return reflect.ValueOf(c[i]).Pointer() < reflect.ValueOf(c[j]).Pointer()
}

func TestMapSortRendering(t *testing.T) {
	type namedMapType map[int]struct{ a int }
	type mapKey struct{ a, b int }

	chans := make(chanList, 5)
	for i := range chans {
		chans[i] = make(chan int)
	}

	tcs := []struct {
		in     interface{}
		expect string
	}{
		{
			map[uint32]struct{}{1: {}, 2: {}, 3: {}, 4: {}, 5: {}, 6: {}, 7: {}, 8: {}},
			"map[uint32]struct {}{1:{}, 2:{}, 3:{}, 4:{}, 5:{}, 6:{}, 7:{}, 8:{}}",
		},
		{
			map[int8]struct{}{1: {}, 2: {}, 3: {}, 4: {}, 5: {}, 6: {}, 7: {}, 8: {}},
			"map[int8]struct {}{1:{}, 2:{}, 3:{}, 4:{}, 5:{}, 6:{}, 7:{}, 8:{}}",
		},
		{
			map[uintptr]struct{}{1: {}, 2: {}, 3: {}, 4: {}, 5: {}, 6: {}, 7: {}, 8: {}},
			"map[uintptr]struct {}{1:{}, 2:{}, 3:{}, 4:{}, 5:{}, 6:{}, 7:{}, 8:{}}",
		},
		{
			namedMapType{10: struct{ a int }{20}},
			"render.namedMapType{10:struct { a int }{20}}",
		},
		{
			map[mapKey]struct{}{mapKey{3, 1}: {}, mapKey{1, 3}: {}, mapKey{1, 2}: {}, mapKey{2, 1}: {}},
			"map[render.mapKey]struct {}{render.mapKey{a:1, b:2}:{}, render.mapKey{a:1, b:3}:{}, render.mapKey{a:2, b:1}:{}, render.mapKey{a:3, b:1}:{}}",
		},
		{
			map[float64]struct{}{10.5: {}, 10.15: {}, 1203: {}, 1: {}, 2: {}},
			"map[float64]struct {}{1:{}, 2:{}, 10.15:{}, 10.5:{}, 1203:{}}",
		},
		{
			map[bool]struct{}{true: {}, false: {}},
			"map[bool]struct {}{false:{}, true:{}}",
		},
		{
			map[interface{}]struct{}{1: {}, 2: {}, 3: {}, "foo": {}},
			`map[interface{}]struct {}{1:{}, 2:{}, 3:{}, "foo":{}}`,
		},
		{
			map[complex64]struct{}{1 + 2i: {}, 2 + 1i: {}, 3 + 1i: {}, 1 + 3i: {}},
			"map[complex64]struct {}{(1+2i):{}, (1+3i):{}, (2+1i):{}, (3+1i):{}}",
		},
		{
			map[chan int]string{nil: "a", chans[0]: "b", chans[1]: "c", chans[2]: "d", chans[3]: "e", chans[4]: "f"},
			`map[(chan int)]string{(chan int)(PTR):"a", (chan int)(PTR):"b", (chan int)(PTR):"c", (chan int)(PTR):"d", (chan int)(PTR):"e", (chan int)(PTR):"f"}`,
		},
	}

	for _, tc := range tcs {
		assertRendersLike(t, reflect.TypeOf(tc.in).Name(), tc.in, tc.expect)
	}
}

func assertRedactsLike(t *testing.T, name string, v interface{}, exp string, opts ...MarshallerOption) {
	var act string
	if opts == nil {
		act = Redact(v)
	} else {
		marshaller, err := NewMarshaller(opts...)
		if err != nil {
			t.Fatalf("Error on creating marshaller: %v", err)
		}
		act = marshaller.Redact(v)
	}
	if act != exp {
		_, _, line, _ := runtime.Caller(1)
		t.Errorf("On line #%d, [%s] did not match expectations:\nExpected: %s\nActual  : %s\n", line, name, exp, act)
	}
}

func TestRedactReplace(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Name string      `redact:"REPLACE"`
		I    interface{} `redact:"REPLACE"`

		m    string `redact:"REPLACE"`
		Test *testStruct
	}

	for i, tc := range []struct {
		a interface{}
		s string
	}{
		{testStruct{Name: "foo", I: &testStruct{Name: "baz"}, m: "randomString", Test: &testStruct{Name: "bar", m: "bob"}},
			`render.testStruct{Name:<redacted>, I:<redacted>, m:<redacted>, Test:(*render.testStruct){Name:<redacted>, I:<redacted>, m:<redacted>, Test:(*render.testStruct)(nil)}}`},
		{[]*testStruct{
			{Name: "foo"},
			{Name: "bar"},
		}, `[]*render.testStruct{(*render.testStruct){Name:<redacted>, I:<redacted>, m:<redacted>, Test:(*render.testStruct)(nil)}, ` +
			`(*render.testStruct){Name:<redacted>, I:<redacted>, m:<redacted>, Test:(*render.testStruct)(nil)}}`},
		{struct {
			a int `redact:"REPLACE"`
			b string
		}{123, "foo"}, `struct { a int "redact:\"REPLACE\""; b string }{<redacted>, "foo"}`},
		{[]interface{}{nil, 1, 2, testStruct{Name: "foo", m: "bar"}}, `[]interface{}{interface{}(nil), 1, 2, render.testStruct{Name:<redacted>, I:<redacted>, m:<redacted>, Test:(*render.testStruct)(nil)}}`},
	} {
		assertRedactsLike(t, fmt.Sprintf("Input #%d", i), tc.a, tc.s)
	}
}

func TestRedactMask(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Name string      `redact:"MASK"`
		I    interface{} `redact:"MASK"`

		m    string `redact:"MASK"`
		Test *testStruct
	}

	for i, tc := range []struct {
		a interface{}
		s string
	}{
		{testStruct{Name: "foo", I: &testStruct{Name: "baz"}, m: "randomString", Test: &testStruct{Name: "bar", m: "bob"}},
			`render.testStruct{Name:"###", I:(*render.testStruct){Name:"###", I:interface{}(nil), m:"", Test:(*render.testStruct)(nil)}, m:"####omString", Test:(*render.testStruct){Name:"###", I:interface{}(nil), m:"###", Test:(*render.testStruct)(nil)}}`},
		{[]*testStruct{
			{Name: "foo"},
			{Name: "bar"},
		}, `[]*render.testStruct{(*render.testStruct){Name:"###", I:interface{}(nil), m:"", Test:(*render.testStruct)(nil)}, ` +
			`(*render.testStruct){Name:"###", I:interface{}(nil), m:"", Test:(*render.testStruct)(nil)}}`},
		{struct {
			a int `redact:"MASK"`
			b string
		}{123456, "foo"}, `struct { a int "redact:\"MASK\""; b string }{####56, "foo"}`},
		{[]interface{}{nil, 1, 2, testStruct{Name: "foo", m: "bar"}}, `[]interface{}{interface{}(nil), 1, 2, render.testStruct{Name:"###", I:interface{}(nil), m:"###", Test:(*render.testStruct)(nil)}}`},
	} {
		assertRedactsLike(t, fmt.Sprintf("Input #%d", i), tc.a, tc.s)
	}
}

func TestRedactRemove(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Name string      `redact:"REMOVE"`
		I    interface{} `redact:"REMOVE"`

		m    string `redact:"REMOVE"`
		Test *testStruct
	}

	for i, tc := range []struct {
		a interface{}
		s string
	}{
		{testStruct{Name: "foo", I: &testStruct{Name: "baz"}, m: "randomString", Test: &testStruct{Name: "bar", m: "bob"}},
			`render.testStruct{Test:(*render.testStruct){Test:(*render.testStruct)(nil)}}`},
		{[]*testStruct{
			{Name: "foo"},
			{Name: "bar"},
		}, `[]*render.testStruct{(*render.testStruct){Test:(*render.testStruct)(nil)}, ` +
			`(*render.testStruct){Test:(*render.testStruct)(nil)}}`},
		{struct {
			a int `redact:"REMOVE"`
			b string
		}{123456, "foo"}, `struct { a int "redact:\"REMOVE\""; b string }{"foo"}`},
		{[]interface{}{nil, 1, 2, testStruct{Name: "foo", m: "bar"}}, `[]interface{}{interface{}(nil), 1, 2, render.testStruct{Test:(*render.testStruct)(nil)}}`},
	} {
		assertRedactsLike(t, fmt.Sprintf("Input #%d", i), tc.a, tc.s)
	}
}

func TestOptions(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Name string `my-tag:"REMOVE"`
		I    interface{}

		m string `my-tag:"MASK"`
	}

	for i, tc := range []struct {
		a interface{}
		s string
	}{
		{testStruct{Name: "foo", I: &testStruct{Name: "baz"}, m: "randomString"},
			`render.testStruct{I:(*render.testStruct){I:interface{}(nil), m:""}, m:"randomStri--"}`},
		{[]*testStruct{
			{Name: "foo"},
			{Name: "bar"},
		}, `[]*render.testStruct{(*render.testStruct){I:interface{}(nil), m:""}, ` +
			`(*render.testStruct){I:interface{}(nil), m:""}}`},
		{struct {
			a int `my-tag:"REPLACE"`
			b string
		}{123, "foo"}, `struct { a int "my-tag:\"REPLACE\""; b string }{<••••>, "foo"}`},
		{[]interface{}{nil, 1, 2, testStruct{Name: "foo", m: "m"}}, `[]interface{}{interface{}(nil), 1, 2, render.testStruct{I:interface{}(nil), m:"-"}}`},
	} {
		assertRedactsLike(t, fmt.Sprintf("Input #%d", i), tc.a, tc.s, WithRedactTag("my-tag"), WithReplacementPlaceholder("••••"), WithMaskingLength(2), WithMaskingReverse(), WithMaskingChar('-'))
	}
}

func TestRegisteredTypes(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		a string
		b int
	}

	for i, tc := range []struct {
		a interface{}
		s string
	}{
		{testStruct{a: "foo", b: 42},
			`render.testStruct(foo#42)`},
		{(*testStruct)(nil), `(*render.testStruct)(nil)`},
		{[]int{1, 2, 3, 4, 5}, `[]int(1/2/3/4/5)`},
		{time.Unix(0, 0), fmt.Sprintf(`time.Time(%s)`, time.Unix(0, 0).Format(time.RFC3339))},
		{"shouldnotpanic", `"shouldnotpanic"`},
	} {
		opts := []MarshallerOption{
			WithTypeFormatter("render.testStruct", func(inter interface{}) string {
				s := inter.(testStruct)
				return fmt.Sprintf("%s#%d", s.a, s.b)
			}),
			WithTypeFormatter("[]int", func(inter interface{}) string {
				a := inter.([]int)
				as := make([]string, len(a))
				for i, ai := range a {
					as[i] = strconv.Itoa(ai)
				}
				return strings.Join(as, "/")
			}),
			WithTypeFormatter("time.Time", func(inter interface{}) string {
				return inter.(time.Time).Format(time.RFC3339)
			}),
			WithTypeFormatter("string", func(inter interface{}) string {
				// this panics when called with a string that does not implement error interface
				return inter.(error).Error()
			}),
			nil, // nil option should not affect marshaller
		}
		assertRedactsLike(t, fmt.Sprintf("Input #%d", i), tc.a, tc.s, opts...)
	}
}
