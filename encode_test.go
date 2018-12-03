package fixedwidth

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"reflect"
	"testing"

	"github.com/pkg/errors"
)

func ExampleMarshal() {
	// define some data to encode
	people := []struct {
		ID        int     `fixed:"1,5"`
		FirstName string  `fixed:"6,15"`
		LastName  string  `fixed:"16,25"`
		Grade     float64 `fixed:"26,30"`
	}{
		{1, "Ian", "Lopshire", 99.5},
	}

	data, err := Marshal(people)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s", data)
	// Output:
	// 1    Ian       Lopshire  99.50
}

func TestMarshal(t *testing.T) {
	type H struct {
		F1 interface{} `fixed:"1,5"`
		F2 interface{} `fixed:"6,10"`
	}
	tagHelper := struct {
		Valid       string `fixed:"1,5"`
		NoTags      string
		InvalidTags string `fixed:"5"`
	}{"foo", "foo", "foo"}
	marshalError := errors.New("marshal error")

	for _, tt := range []struct {
		name      string
		i         interface{}
		o         []byte
		shouldErr bool
	}{
		{"single line", H{"foo", 1}, []byte("foo  1    "), false},
		{"multiple line", []H{{"foo", 1}, {"bar", 2}}, []byte("foo  1    \nbar  2    "), false},
		{"empty slice", []H{}, nil, false},
		{"pointer", &H{"foo", 1}, []byte("foo  1    "), false},
		{"nil", nil, nil, false},
		{"invalid type", true, nil, true},
		{"invalid type in struct", H{"foo", true}, nil, true},
		{"marshal error", EncodableString{"", marshalError}, nil, true},
		{"invalid tags", tagHelper, []byte("foo  "), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o, err := Marshal(tt.i)
			if tt.shouldErr != (err != nil) {
				t.Errorf("Marshal() shouldErr expected %v, have %v (%v)", tt.shouldErr, err != nil, err)
			}
			if !tt.shouldErr && !bytes.Equal(o, tt.o) {
				t.Errorf("Marshal() expected %s, have %s", tt.o, o)
			}
		})
	}
}

type DifferentFixedTags struct {
	RegularStr string `fixed:"1,5"`
	RegularInt int    `fixed:"6,10"`

	PadStr string `fixed:"11,15,leftpad"`
	PadInt int    `fixed:"16,20,leftpad"`

	PadPtrStr *string `fixed:"21,30,leftpad"`
	PadPtrInt *int    `fixed:"31,40,leftpad"`
}

func TestNewValueEncoder(t *testing.T) {
	threeStr := "three"
	threeInt := 3
	for _, tt := range []struct {
		name      string
		i         interface{}
		o         []byte
		shouldErr bool
	}{
		{"nil", nil, []byte(""), false},
		{"nil interface", interface{}(nil), []byte(""), false},

		{"[]string (invalid)", []string{"a", "b"}, []byte(""), true},
		{"[]string interface (invalid)", interface{}([]string{"a", "b"}), []byte(""), true},
		{"bool (invalid)", true, []byte(""), true},

		{"string", "foo", []byte("foo"), false},
		{"string interface", interface{}("foo"), []byte("foo"), false},
		{"string empty", "", []byte(""), false},
		{"*string", stringp("foo"), []byte("foo"), false},
		{"*string empty", stringp(""), []byte(""), false},
		{"*string nil", nilString, []byte(""), false},

		{"float64", float64(123.4567), []byte("123.46"), false},
		{"float64 interface", interface{}(float64(123.4567)), []byte("123.46"), false},
		{"float64 zero", float64(0), []byte("0.00"), false},
		{"*float64", float64p(123.4567), []byte("123.46"), false},
		{"*float64 zero", float64p(0), []byte("0.00"), false},
		{"*float64 nil", nilFloat64, []byte(""), false},

		{"float32", float32(123.4567), []byte("123.46"), false},
		{"float32 interface", interface{}(float32(123.4567)), []byte("123.46"), false},
		{"float32 zero", float32(0), []byte("0.00"), false},
		{"*float32", float32p(123.4567), []byte("123.46"), false},
		{"*float32 zero", float32p(0), []byte("0.00"), false},
		{"*float32 nil", nilFloat32, []byte(""), false},

		{"int", int(123), []byte("123"), false},
		{"int interface", interface{}(int(123)), []byte("123"), false},
		{"int zero", int(0), []byte("0"), false},
		{"*int", intp(123), []byte("123"), false},
		{"*int zero", intp(0), []byte("0"), false},
		{"*int nil", nilInt, []byte(""), false},

		{"TextUnmarshaler", EncodableString{"foo", nil}, []byte("foo"), false},
		{"TextUnmarshaler interface", interface{}(EncodableString{"foo", nil}), []byte("foo"), false},
		{"TextUnmarshaler error", EncodableString{"foo", errors.New("TextUnmarshaler error")}, []byte("foo"), true},

		{"DifferentFixedTags", DifferentFixedTags{"one", 1, "two", 2, &threeStr, &threeInt}, []byte("one  1      two00002     three0000000003"), false},
		{"DifferentFixedTags without values", DifferentFixedTags{}, []byte("     0         00000          0000000000"), false},
		{"DifferentFixedTags full pad", DifferentFixedTags{"first", 12345, "secnd", 67890, &threeStr, &threeInt}, []byte("first12345secnd67890     three0000000003"), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o, err := newValueEncoder(reflect.TypeOf(tt.i))(reflect.ValueOf(tt.i))
			if tt.shouldErr != (err != nil) {
				t.Errorf("newValueEncoder(%s)() shouldErr expected %v, have %v (%v)", reflect.TypeOf(tt.i).Name(), tt.shouldErr, err != nil, err)
			}
			if !tt.shouldErr && !bytes.Equal(o, tt.o) {
				t.Errorf("newValueEncoder(%s)()\nexpected %q\nreceived %q", reflect.TypeOf(tt.i).Name(), tt.o, o)
			}
		})
	}
}

func TestEncoderWithMultipleLines(t *testing.T) {
	input := []interface{}{
		EncodableString{"foo", nil},
		EncodableString{"bar", nil},
	}

	for _, tt := range []struct {
		name       string
		expected   string
		newEncoder func(io.Writer) *Encoder
	}{
		{"default", "foo\nbar", func(w io.Writer) *Encoder { return NewEncoder(w) }},
		{"default", "foo\r\nbar", func(w io.Writer) *Encoder {
			enc := NewEncoder(w)
			enc.LineEnd = []byte("\r\n")
			return enc
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := tt.newEncoder(&buf)
			err := enc.Encode(input)
			if err != nil {
				t.Fatal(err)
			}
			if buf.String() != tt.expected {
				t.Fatalf("unexpected output\nexpected: %q\nreceived: %q", tt.expected, buf.String())
			}
		})
	}
}
