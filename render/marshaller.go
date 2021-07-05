package render

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
)

// Default values
const (
	DefaultRedactTag              = "redact"
	DefaultReplacementPlaceholder = "redacted"
	DefaultRecursionPlaceholder   = "recursive"
	DefaultMaskingChar            = '#'
	DefaultMaskingLength          = 4
)

var defaultMaskingCharToBytes = make([]byte, utf8.RuneLen(DefaultMaskingChar))

func init() {
	utf8.EncodeRune(defaultMaskingCharToBytes, DefaultMaskingChar)
}

// Redact modes
const (
	REMOVE  = "REMOVE"
	REPLACE = "REPLACE"
	MASK    = "MASK"
)

// Marshaller allow to configure options for rendering or redacting
type Marshaller struct {
	options *options
}

var defaultRenderOptions = renderOptions{
	typeFormatters:       make(map[string]func(interface{}) string),
	recursionPlaceholder: DefaultRecursionPlaceholder,
}
var defaultRedactOptions = redactOptions{
	active:                 false,
	tag:                    DefaultRedactTag,
	replacementPlaceholder: DefaultReplacementPlaceholder,
	maskingChar:            defaultMaskingCharToBytes,
	maskingLength:          DefaultMaskingLength,
	maskingReverse:         false,
}

func newDefaultMarshaller() *Marshaller {
	return &Marshaller{
		options: &options{
			render: defaultRenderOptions,
			redact: defaultRedactOptions,
		},
	}
}

// MarshallerOption configures the Marshaller
type MarshallerOption func(m *Marshaller) error

// NewMarshaller creates a Marshaller with optional otions With...()
func NewMarshaller(opts ...MarshallerOption) (*Marshaller, error) {
	m := newDefaultMarshaller()
	for _, opt := range opts {
		if opt != nil {
			err := opt(m)
			if err != nil {
				return nil, err
			}
		}
	}
	return m, nil
}

// WithTypeFormatter lets you set a specific formatter for a given type.
// The formatter is called before redacting data so it is your responsibility
// to redact yourself the data in the given formatter.
// In case the formatter you gave panics, the marshaller will recover and treat
// the type as a regular one.
//
// Example:
//  WithTypeFormatter("time.Time", func (t interface{}) string {
//    return t.(time.Time).Format(time.RFC3339)
//  })
func WithTypeFormatter(typeName string, typeFormatter func(interface{}) string) MarshallerOption {
	return func(m *Marshaller) error {
		m.options.render.typeFormatters[typeName] = typeFormatter
		return nil
	}
}

// WithTypeFormatters lets you set specific formatters by batch
// See WithTypeFormatter for more details
func WithTypeFormatters(typeFormatters map[string]func(interface{}) string) MarshallerOption {
	return func(m *Marshaller) error {
		for typeName, typeFormatter := range typeFormatters {
			err := WithTypeFormatter(typeName, typeFormatter)(m)
			if err != nil {
				return nil
			}
		}
		return nil
	}
}

// WithRecursionPlaceholder lets you set the placeholder used when a recursive
// type has been detected. The placeholder will be surrounded by "<" and ">."
//
// The default value for this placeholder is "recursive"
func WithRecursionPlaceholder(recursionPlaceholder string) MarshallerOption {
	return func(m *Marshaller) error {
		err := validateRecursiveString(recursionPlaceholder)
		if err != nil {
			return errors.Wrap(err, "invalid recursion placeholder")
		}
		m.options.render.recursionPlaceholder = recursionPlaceholder
		return nil
	}
}

// WithRedactTag lets you set the tag used to specify struct fields to redact
//
// The default value for this tag is "redact"
func WithRedactTag(redactTag string) MarshallerOption {
	return func(m *Marshaller) error {
		err := validateTag(redactTag)
		if err != nil {
			return errors.Wrap(err, "invalid redact tag")
		}
		m.options.redact.tag = redactTag
		return nil
	}
}

// WithReplacementPlaceholder lets you set the placeholder used when the redacting mode is
// set to "REPLACE". The placeholder will be surrounded by "<" and ">."
//
// The default value for this placeholder is "redacted"
func WithReplacementPlaceholder(replacementPlaceholder string) MarshallerOption {
	return func(m *Marshaller) error {
		err := validateReplacementString(replacementPlaceholder)
		if err != nil {
			return errors.Wrap(err, "invalid replacement placeholder")
		}
		m.options.redact.replacementPlaceholder = replacementPlaceholder
		return nil
	}
}

// WithMaskingChar lets you set the character used to mask when the redacting
// mode is set to "MASK".
//
// The default value for this character is '#'
func WithMaskingChar(maskingChar rune) MarshallerOption {
	return func(m *Marshaller) error {
		m.options.redact.maskingChar = make([]byte, utf8.RuneLen(maskingChar))
		utf8.EncodeRune(m.options.redact.maskingChar, maskingChar)
		return nil
	}
}

// WithMaskingLength lets you set the max length of the mask when the redacting
// mode is set to "MASK"
//
// The default value for this character is 4. A negative value will mask all
// characters.
func WithMaskingLength(maskingLength int) MarshallerOption {
	return func(m *Marshaller) error {
		m.options.redact.maskingLength = maskingLength
		return nil
	}
}

// WithMaskingReverse lets you set the values to be masked from the end and not
// from the start when the redacting mode is set to "MASK"
func WithMaskingReverse() MarshallerOption {
	return func(m *Marshaller) error {
		m.options.redact.maskingReverse = true
		return nil
	}
}

// Render converts a structure to a string representation. Unlike the "%#v"
// format string, this resolves pointer types' contents in structs, maps, and
// slices/arrays and prints their field values.
func (m *Marshaller) Render(v interface{}) string {
	s := (*traverseState)(nil)
	opts := m.options
	opts.redact.active = false
	str := NewDefaultMaskWriter(false, opts.redact)
	s.render(str, 0, reflect.ValueOf(v), false, opts)
	return str.String()
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
func (m *Marshaller) Redact(v interface{}) string {
	s := (*traverseState)(nil)
	opts := m.options
	opts.redact.active = true
	str := NewDefaultMaskWriter(false, opts.redact)
	s.render(str, 0, reflect.ValueOf(v), false, opts)
	return str.String()
}

// RenderProto converts a structure to a string representation. Unlike the "%#v"
// format string, this resolves pointer types' contents in structs, maps, and
// slices/arrays and prints their field values.
func (m *Marshaller) RenderProto(v proto.Message) string {
	str := strings.Builder{}
	s := (*traverseState)(nil)
	opts := m.options
	opts.redact.active = false
	s.renderProtoMessage(&str, v.ProtoReflect(), false, opts)
	return str.String()
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
func (m *Marshaller) RedactProto(v proto.Message) string {
	str := strings.Builder{}
	s := (*traverseState)(nil)
	opts := m.options
	opts.redact.active = true
	s.renderProtoMessage(&str, v.ProtoReflect(), false, opts)
	return str.String()
}

var tagRegexString = "^[a-zA-Z0-9_-]+$"
var tagRegex = regexp.MustCompile(tagRegexString)

func validateRecursiveString(recursionPlaceholder string) error {
	return nil
}
func validateReplacementString(redactedString string) error {
	return nil
}
func validateTag(tag string) error {
	if !tagRegex.MatchString(tag) {
		return fmt.Errorf("must validate: %s", tagRegexString)
	}
	return nil
}
