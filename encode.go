package fixedwidth

import (
	"bufio"
	"bytes"
	"encoding"
	"io"
	"reflect"
	"strconv"
)

// Marshal returns the fixed-width encoding of v.
//
// v must be an encodable type or a slice of an encodable
// type. If v is a slice, each item will be treated as a
// line. If v is a single encodable type, a single line
// will be encoded.
//
// In order for a type to be encodable, it must implement
// the encoding.TextMarshaler interface or be based on one
// of the following builtin types: string, int, int64,
// int32, int16, int8, float64, float32, or struct.
// Pointers to encodable types and interfaces containing
// encodable types are also encodable.
//
// nil pointers and interfaces will be omitted. zero vales
// will be encoded normally.
//
// A struct is encoded to a single slice of bytes. Each
// field in a struct will be encoded and placed at the
// position defined by its struct tags. The tags should be
// formatted as `fixed:"{startPos},{endPos}"`. Positions
// start at 1. The interval is inclusive. Fields without
// tags and Fields of an un-encodable type are ignored.
//
// If the encoded value of a field is longer than the
// length of the position interval, the overflow is
// truncated.
func Marshal(v interface{}) ([]byte, error) {
	buff := bytes.NewBuffer(nil)
	err := NewEncoder(buff).Encode(v)
	if err != nil {
		return nil, err
	}
	return buff.Bytes(), nil
}

// MarshalInvalidTypeError describes an invalid type being marshaled.
type MarshalInvalidTypeError struct {
	typeName string
}

func (e *MarshalInvalidTypeError) Error() string {
	return "fixedwidth: cannot marshal unknown Type " + e.typeName
}

// An Encoder writes fixed-width formatted data to an output
// stream.
type Encoder struct {
	w *bufio.Writer

	LineEnd []byte
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{
		w:       bufio.NewWriter(w),
		LineEnd: []byte("\n"),
	}
}

// Encode writes the fixed-width encoding of v to the
// stream.
// See the documentation for Marshal for details about
// encoding behavior.
func (e *Encoder) Encode(i interface{}) (err error) {
	if i == nil {
		return nil
	}

	// check to see if i should be encoded into multiple lines
	v := reflect.ValueOf(i)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice {
		// encode each slice element to a line
		err = e.writeLines(v)
	} else {
		// this is a single object so encode the original vale to a line
		err = e.writeLine(reflect.ValueOf(i))
	}
	if err != nil {
		return err
	}
	return e.w.Flush()
}

func (e *Encoder) writeLines(v reflect.Value) error {
	lineEnd := e.LineEnd
	if len(lineEnd) == 0 {
		lineEnd = []byte("\n")
	}
	for i := 0; i < v.Len(); i++ {
		err := e.writeLine(v.Index(i))
		if err != nil {
			return err
		}

		if i != v.Len()-1 {
			_, err := e.w.Write(lineEnd)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Encoder) writeLine(v reflect.Value) (err error) {
	b, err := newValueEncoder(v.Type())(v)
	if err != nil {
		return err
	}
	_, err = e.w.Write(b)
	return err
}

type valueEncoder func(v reflect.Value) ([]byte, error)

func newValueEncoder(t reflect.Type) valueEncoder {
	if t == nil {
		return nilEncoder
	}
	if t.Implements(reflect.TypeOf(new(encoding.TextMarshaler)).Elem()) {
		return textMarshalerEncoder
	}

	switch t.Kind() {
	case reflect.Ptr, reflect.Interface:
		return ptrInterfaceEncoder
	case reflect.Struct:
		return structEncoder
	case reflect.String:
		return stringEncoder
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
		return intEncoder
	case reflect.Float64:
		return floatEncoder(2, 64)
	case reflect.Float32:
		return floatEncoder(2, 32)
	}
	return unknownTypeEncoder(t)
}

func structEncoder(v reflect.Value) ([]byte, error) {
	var specs []fieldSpec
	for i := 0; i < v.Type().NumField(); i++ {
		f := v.Type().Field(i)
		var (
			err  error
			spec fieldSpec
		)
		spec, err = parseTag(f.Tag.Get("fixed"))
		if err != nil {
			continue
		}
		value, err := newValueEncoder(f.Type)(v.Field(i))
		if err != nil {
			return nil, err
		}
		if len(value) < spec.fieldLength() && spec.leftpad {
			spec.value = bytes.Repeat([]byte{spec.leftpadChar(f.Type)}, spec.fieldLength())
			copy(spec.value[spec.fieldLength()-len(value):len(spec.value)], value)
		} else {
			spec.value = value
		}
		specs = append(specs, spec)
	}
	return encodeSpecs(specs), nil
}

type fieldSpec struct {
	startPos, endPos int
	leftpad          bool
	value            []byte
}

func (f fieldSpec) fieldLength() int {
	return f.endPos - f.startPos + 1
}

func (f fieldSpec) leftpadChar(t reflect.Type) byte {
	if t.Implements(reflect.TypeOf(new(encoding.TextMarshaler)).Elem()) {
		return ' '
	}

	switch t.Kind() {
	case reflect.Ptr:
		return f.leftpadChar(t.Elem())
	case reflect.Interface:
		return ' '
	case reflect.Struct:
		return ' '
	case reflect.String:
		return ' '
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
		return '0'
	case reflect.Float32, reflect.Float64:
		return '0'
	default:
		return ' '
	}
}

func encodeSpecs(specs []fieldSpec) []byte {
	var ll int
	for _, spec := range specs {
		if spec.endPos > ll {
			ll = spec.endPos
		}
	}
	data := bytes.Repeat([]byte(" "), ll)
	for _, spec := range specs {
		copy(data[spec.startPos-1:spec.endPos], spec.value)
	}
	return data
}

func textMarshalerEncoder(v reflect.Value) ([]byte, error) {
	return v.Interface().(encoding.TextMarshaler).MarshalText()
}

func ptrInterfaceEncoder(v reflect.Value) ([]byte, error) {
	if v.IsNil() {
		return nilEncoder(v)
	}
	return newValueEncoder(v.Elem().Type())(v.Elem())
}

func stringEncoder(v reflect.Value) ([]byte, error) {
	return []byte(v.String()), nil
}

func intEncoder(v reflect.Value) ([]byte, error) {
	return []byte(strconv.Itoa(int(v.Int()))), nil
}

func floatEncoder(perc, bitSize int) valueEncoder {
	return func(v reflect.Value) ([]byte, error) {
		return []byte(strconv.FormatFloat(v.Float(), 'f', perc, bitSize)), nil
	}
}

func nilEncoder(v reflect.Value) ([]byte, error) {
	return nil, nil
}

func unknownTypeEncoder(t reflect.Type) valueEncoder {
	return func(value reflect.Value) ([]byte, error) {
		return nil, &MarshalInvalidTypeError{typeName: t.Name()}
	}
}
