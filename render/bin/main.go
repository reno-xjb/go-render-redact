package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/reno-xjb/go-render-redact/render"
	"github.com/reno-xjb/go-render-redact/render/protocol/redact"
)

func main() {
	// t := time.Now()
	m, err := render.NewMarshaller(
		render.WithTypeFormatter("int", func(t interface{}) string {
			return strconv.Itoa(t.(int)) + "xx"
		}),
		nil,
		render.WithTypeFormatter("time.Time", func(t interface{}) string {
			return t.(time.Time).Format(time.RFC3339)
		}),
	)
	fmt.Println(err)
	fmt.Println(m.Redact(42))

	type MyStruct struct {
		a interface{}
		b *MyStruct `redact:"MASK"`
	}

	s := MyStruct{
		a: 1,
		b: &MyStruct{
			a: []int{1, 12, 123, 1234, 12345},
			b: &MyStruct{
				a: map[string]string{
					"key": "value",
				},
				b: &MyStruct{
					a: time.Now(),
					b: nil,
				},
			},
		},
	}
	fmt.Println(m.Redact(s))

	r := &redact.Recursive{
		TestString: "test",
		R:          nil,
	}
	r.R = r
	fmt.Println(m.RenderProto(r))
}
