package render

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/pkg/errors"
)

// Default values
const (
	TAG       = "redact"
	REDACTED  = "redacted"
	RECURSIVE = "recursive"
)

// Constants
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
	registeredTypes: make(map[string]func(interface{}) string),
	recursiveString: RECURSIVE,
}
var defaultRedactOptions = redactOptions{
	active:            false,
	tag:               TAG,
	replacementString: REDACTED,
	maskingChar:       '#',
	maskingLength:     4,
	maskingReverse:    false,
}

func newDefaultMarshaller() *Marshaller {
	return &Marshaller{
		options: &options{
			render: defaultRenderOptions,
			redact: defaultRedactOptions,
		},
	}
}

// Options allow to configure render
type Options struct {
	RenderRecursiveString   string
	RenderRegisteredTypes   map[string]func(interface{}) string
	RedactTag               string
	RedactReplacementString string
	RedactMaskingChar       rune
	RedactMaskingLength     int
	RedactMaskingReverse    bool
}

// NewMarshaller creates a Marshaller with custom options
func NewMarshaller(opts *Options) (*Marshaller, error) {
	m := newDefaultMarshaller()
	if opts == nil {
		return m, nil
	}
	if len(opts.RenderRegisteredTypes) != 0 {
		m.options.render.registeredTypes = opts.RenderRegisteredTypes
	}
	if len(opts.RenderRecursiveString) != 0 {
		err := validateRecursiveString(opts.RenderRecursiveString)
		if err != nil {
			return nil, errors.Wrap(err, "invalid 'RedactRecursiveString' option")
		}
		m.options.render.recursiveString = opts.RenderRecursiveString
	}

	if len(opts.RedactTag) != 0 {
		err := validateTag(opts.RedactTag)
		if err != nil {
			return nil, errors.Wrap(err, "invalid 'RedactTag' option")
		}
		m.options.redact.tag = opts.RedactTag
	}

	if len(opts.RedactReplacementString) != 0 {
		err := validateReplacementString(opts.RedactReplacementString)
		if err != nil {
			return nil, errors.Wrap(err, "invalid 'RedactRedactedString' option")
		}
		m.options.redact.replacementString = opts.RedactReplacementString
	}

	if opts.RedactMaskingChar != 0 {
		m.options.redact.maskingChar = opts.RedactMaskingChar
	}

	if opts.RedactMaskingLength != 0 {
		m.options.redact.maskingLength = opts.RedactMaskingLength
	}

	if opts.RedactMaskingReverse {
		m.options.redact.maskingReverse = opts.RedactMaskingReverse
	}

	return m, nil
}

// Render converts a structure to a string representation. Unlike the "%#v"
// format string, this resolves pointer types' contents in structs, maps, and
// slices/arrays and prints their field values.
func (m *Marshaller) Render(v interface{}) string {
	str := strings.Builder{}
	s := (*traverseState)(nil)
	s.render(&str, 0, reflect.ValueOf(v), false, false, m.options)
	return str.String()
}

// Redact converts a structure to a string representation. Unlike the "%#v"
// format string, this resolves pointer types' contents in structs, maps, and
// slices/arrays and prints their field values.
// It also redacts fields that are marke with a specific tag, that can be
// configured in options
func (m *Marshaller) Redact(v interface{}) string {
	str := strings.Builder{}
	s := (*traverseState)(nil)
	opts := m.options
	opts.redact.active = true
	s.render(&str, 0, reflect.ValueOf(v), false, false, opts)
	return str.String()
}

var tagRegexString = "^[a-zA-Z0-9_-]+$"
var tagRegex = regexp.MustCompile(tagRegexString)

func validateRecursiveString(recursiveString string) error {
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
