package engine

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

var (
	errExist = errors.New("exist")

	ErrNotSupportType      = errors.New("cannot support type")
	ErrNilInterface        = errors.New("interface is nil")
	ErrPointerToUnexported = errors.New("cannot set embedded pointer to unexported struct")
	ErrInvalidFormat       = errors.New("the raw data has an invalid format for an object value")
)

// field represents a single field found in a struct.
type field[T any] struct {
	index     int
	name      string
	typ       reflect.Type
	meta      *T
	omitEmpty bool
	encoder   encoderFunc[T]
	decoder   decoderFunc[T]
	embedded  structFields[T]
}

type structFields[T any] []field[T]

var fieldCache sync.Map // map[reflect.Type]structFields[T]

// cachedFields is like typeFields but uses a cache to avoid repeated work.
func (e *engine[T]) cachedFields(t reflect.Type) structFields[T] {
	if c, ok := fieldCache.Load(t); ok {
		return c.(structFields[T])
	}
	c, _ := fieldCache.LoadOrStore(t, e.typeFields(t))
	return c.(structFields[T])
}

// typeFields returns a list of fields that the encoder should recognize for the given type.
func (e *engine[T]) typeFields(t reflect.Type) structFields[T] {
	var err error

	fields := make(structFields[T], 0, t.NumField())

	// Scan v for fields to encode.
	for i := 0; i < t.NumField(); i++ {
		structField := t.Field(i)
		fieldType := structField.Type

		fld := field[T]{
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

			fld.meta = new(T)
			if fld.omitEmpty, err = e.Parse(tag, fld.meta); err != nil {
				fld.encoder, fld.decoder = invalidTagEncoder[T](tag, err), invalidTagDecoder[T](tag, err)
				return append(fields, fld)
			}
		}

		fld.encoder, fld.decoder = e.typeCoders(fieldType)
		fields = append(fields, fld)
	}

	return fields
}

// typeCoders returns encoderFunc and decoderFunc for a type.
func (e *engine[T]) typeCoders(t reflect.Type) (ef encoderFunc[T], df decoderFunc[T]) {
	if t.Kind() != reflect.Pointer {
		p := reflect.PointerTo(t)
		if p.Implements(e.marshaller) {
			ef = marshallerEncoder[T]
		}
		if p.Implements(e.unmarshaler) {
			df = unmarshalerDecoder[T]
			if ef != nil {
				return
			}
		}
	}

	switch t.Kind() {
	case reflect.Bool:
		return setCoder[T](ef, boolEncoder[T]), setCoder[T](df, boolDecoder[T])
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return setCoder[T](ef, intEncoder[T]), setCoder[T](df, intDecoder[T])
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return setCoder[T](ef, uintEncoder[T]), setCoder[T](df, uintDecoder[T])
	case reflect.Float32, reflect.Float64:
		return setCoder[T](ef, floatEncoder[T]), setCoder[T](df, floatDecoder[T])
	//case reflect.Array:
	//	return setCoder[T](ef, arrayEncoder[T]), setCoder[T](df, arrayDecoder[T])
	case reflect.Interface:
		return setCoder[T](ef, interfaceEncoder[T]), setCoder[T](df, interfaceDecoder[T])
	//case reflect.Map:
	//	return setCoder[T](ef, mapEncoder[T]), setCoder[T](df, mapDecoder[T])
	case reflect.Pointer:
		return setCoder[T](ef, pointerEncoder[T]), setCoder[T](df, pointerDecoder[T])
	case reflect.Slice:
		return sliceCoders(t, ef, df)
	case reflect.String:
		return setCoder[T](ef, stringEncoder[T]), setCoder[T](df, stringDecoder[T])
	case reflect.Struct:
		return setCoder[T](ef, structEncoder[T]), setCoder[T](df, structDecoder[T])
	default:
		return setCoder[T](ef, unsupportedTypeEncoder[T]), setCoder[T](df, unsupportedTypeDecoder[T])
	}
}

func setCoder[T any, F encoderFunc[T] | decoderFunc[T]](i, f F) F {
	if i != nil {
		return i
	}
	return f
}

func sliceCoders[T any](t reflect.Type, ef encoderFunc[T], df decoderFunc[T]) (encoderFunc[T], decoderFunc[T]) {
	if t.Elem().Kind() == reflect.Uint8 {
		return setCoder[T](ef, bytesEncoder[T]), setCoder[T](df, bytesDecoder[T])
	} else {
		return setCoder[T](ef, unsupportedTypeEncoder[T]), setCoder[T](df, unsupportedTypeDecoder[T])
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

type context[T any] struct {
	structName string
	field      field[T]
	err        error
}

func (c *context[T]) setError(tagName, state string, err error) {
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
