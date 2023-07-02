package engine

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

var (
	ErrNotSupportType      = errors.New("cannot support type")
	ErrNilInterface        = errors.New("interface is nil")
	ErrPointerToUnexported = errors.New("cannot set embedded pointer to unexported struct")
	ErrInvalidFormat       = errors.New("the raw data has an invalid format for an object value")
)

// field represents a single field found in a struct.
type field struct {
	index     int
	name      string
	typ       reflect.Type
	tag       string
	omitEmpty bool
	encoder   encoderFunc
	decoder   decoderFunc
	embedded  structFields
}

type structFields []field

var fieldCache sync.Map // map[reflect.Type]structFields

// cachedFields is like typeFields but uses a cache to avoid repeated work.
func (e *engine) cachedFields(t reflect.Type) structFields {
	if c, ok := fieldCache.Load(t); ok {
		return c.(structFields)
	}
	c, _ := fieldCache.LoadOrStore(t, e.typeFields(t))
	return c.(structFields)
}

// typeFields returns a list of fields that the encoder should recognize for the given type.
func (e *engine) typeFields(t reflect.Type) structFields {
	fields := make(structFields, 0, t.NumField())

	// Scan v for fields to encode.
	for i := 0; i < t.NumField(); i++ {
		structField := t.Field(i)
		fieldType := structField.Type

		fld := field{
			index: i,
			name:  structField.Name,
			typ:   fieldType,
		}

		if structField.Anonymous {
			if fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}

			// Ignore embedded fields of unexported non-struct types.
			if !structField.IsExported() && fieldType.Kind() != reflect.Struct {
				continue
			}

			// Do not ignore embedded fields of unexported struct types since they may have exported fields.
			fld.embedded = e.typeFields(fieldType)

			if fld.embedded == nil {
				continue
			}

			fields = append(fields, fld)
			continue
		} else if !structField.IsExported() {
			// Ignore unexported non-embedded fields.
			continue
		}

		if tag, ok := structField.Tag.Lookup(e.Name()); ok {
			// Ignore the field if the tag has a skip fieldValue.
			if e.Skip(tag) {
				continue
			}

			fld.tag = tag
			fld.omitEmpty = e.Omitempty(tag)
		}

		fld.encoder, fld.decoder = e.typeCoders(fieldType)
		fields = append(fields, fld)
	}

	return fields
}

// typeCoders returns decoderFunc and encoderFunc for a type.
func (e *engine) typeCoders(t reflect.Type) (ef encoderFunc, df decoderFunc) {
	if t.Kind() != reflect.Pointer {
		p := reflect.PointerTo(t)
		if p.Implements(e.marshaller) {
			ef = marshallerEncoder
		}
		if p.Implements(e.unmarshaler) {
			df = unmarshalerDecoder
			if ef != nil {
				return
			}
		}
	}

	switch t.Kind() {
	case reflect.Bool:
		return setCoder(ef, boolEncoder), setCoder(df, boolDecoder)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return setCoder(ef, intEncoder), setCoder(df, intDecoder)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return setCoder(ef, uintEncoder), setCoder(df, uintDecoder)
	case reflect.Float32, reflect.Float64:
		return setCoder(ef, floatEncoder), setCoder(df, floatDecoder)
	//case reflect.Array:
	//	return setCoder(ef, arrayEncoder), setCoder(df, arrayDecoder)
	case reflect.Interface:
		return setCoder(ef, interfaceEncoder), setCoder(df, interfaceDecoder)
	//case reflect.Map:
	//	return setCoder(ef, mapEncoder), setCoder(df, mapDecoder)
	case reflect.Pointer:
		return setCoder(ef, pointerEncoder), setCoder(df, pointerDecoder)
	case reflect.Slice:
		return sliceCoders(t, ef, df)
	case reflect.String:
		return setCoder(ef, stringEncoder), setCoder(df, stringDecoder)
	case reflect.Struct:
		return setCoder(ef, structEncoder), setCoder(df, structDecoder)
	default:
		return setCoder(ef, unsupportedTypeEncoder), setCoder(df, unsupportedTypeDecoder)
	}
}

func setCoder[T encoderFunc | decoderFunc](i, f T) T {
	if i != nil {
		return i
	}
	return f
}

func sliceCoders(t reflect.Type, ef encoderFunc, df decoderFunc) (encoderFunc, decoderFunc) {
	if t.Elem().Kind() == reflect.Uint8 {
		return setCoder(ef, bytesEncoder), setCoder(df, bytesDecoder)
	} else {
		return setCoder(ef, unsupportedTypeEncoder), setCoder(df, unsupportedTypeDecoder)
	}
}

func bitSize(v reflect.Kind) int {
	switch v {
	case reflect.Int8, reflect.Uint8:
		return 8
	case reflect.Int16, reflect.Uint16:
		return 16
	case reflect.Int32, reflect.Uint32, reflect.Float32:
		return 32
	case reflect.Int64, reflect.Uint64, reflect.Float64:
		return 64
	case reflect.Int, reflect.Uint, reflect.Uintptr:
		return 32 << (^uint(0) >> 63)
	}
	return 0
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}

type context struct {
	structName string
	field      field
	err        error
}

func (c *context) setError(tagName, state string, err error) {
	err = unwrapErr(err)
	if c.structName == "" {
		c.err = fmt.Errorf("%s: cannot %s Go value of type %s: %w", tagName, state, c.field.typ, err)
	} else {
		c.err = fmt.Errorf("%s: cannot %s Go struct field %s.%s of type %s: %w", tagName, state, c.structName, c.field.name, c.field.typ, err)
	}
}

func unwrapErr(err error) error {
	if ew := errors.Unwrap(err); ew != nil {
		return ew
	}
	return err
}
