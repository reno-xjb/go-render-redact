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
	REDACT = "REDACT"
)

// Marshaller allow to configure options for rendering or redacting
type Marshaller struct {
	options *options
}

func newDefaultMarshaller() *Marshaller {
	return &Marshaller{
		options: &options{
			recursiveString: RECURSIVE,
			redact:          false,
			redactTag:       TAG,
			redactedString:  REDACTED,
		},
	}
}

// Options allow to configure render
type Options struct {
	RecursiveString string
	Tag             string
	RedactedString  string
}

// NewMarshaller creates a Marshaller with custom options
func NewMarshaller(opts *Options) (*Marshaller, error) {
	m := newDefaultMarshaller()
	if opts == nil {
		return m, nil
	}
	if len(opts.RecursiveString) != 0 {
		err := validateRecursiveString(opts.RecursiveString)
		if err != nil {
			return nil, errors.Wrap(err, "invalid 'RecursiveString' option")
		}
		m.options.recursiveString = opts.RecursiveString
	}

	if len(opts.RedactedString) != 0 {
		err := validateRedactedString(opts.RedactedString)
		if err != nil {
			return nil, errors.Wrap(err, "invalid 'RedactedString' option")
		}
		m.options.redactedString = opts.RedactedString
	}

	if len(opts.Tag) != 0 {
		err := validateTag(opts.Tag)
		if err != nil {
			return nil, errors.Wrap(err, "invalid 'Tag' option")
		}
		m.options.redactTag = opts.Tag
	}
	return m, nil
}

// Render converts a structure to a string representation. Unlike the "%#v"
// format string, this resolves pointer types' contents in structs, maps, and
// slices/arrays and prints their field values.
func (m *Marshaller) Render(v interface{}) string {
	str := strings.Builder{}
	s := (*traverseState)(nil)
	s.render(&str, 0, reflect.ValueOf(v), false, m.options)
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
	opts.redact = true
	s.render(&str, 0, reflect.ValueOf(v), false, opts)
	return str.String()
}

var tagRegexString = "^[a-zA-Z0-9_-]+$"
var tagRegex = regexp.MustCompile(tagRegexString)

func validateRecursiveString(recursiveString string) error {
	return nil
}
func validateRedactedString(redactedString string) error {
	return nil
}
func validateTag(tag string) error {
	if !tagRegex.MatchString(tag) {
		return fmt.Errorf("must validate: %s", tagRegexString)
	}
	return nil
}
