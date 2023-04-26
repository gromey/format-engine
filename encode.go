package engine

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"sync"
)

const marshalError = "encode data from"

// Marshal encodes the value v and returns the encoded data.
// If v is nil, Marshal returns an encoder error.
func (e engine) Marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("%s: Marshal(nil)", e.Name())
	}

	s := e.newEncodeState()
	//s = s.cachedEncode(v) // TODO it is the wrong way to encode from the same pointer.
	s = s.cache(v)

	return s.buf, s.err
}

type encodeState struct {
	engine
	context
	buf []byte
}

func (e engine) newEncodeState() encodeState {
	return encodeState{engine: e}
}

var encodeCache sync.Map // map[reflect.Type]encodeState

// cachedEncode uses a cache to avoid repeated work.
func (s *encodeState) cachedEncode(v any) encodeState {
	if f, ok := encodeCache.Load(v); ok {
		return f.(encodeState)
	}
	f, _ := encodeCache.LoadOrStore(v, s.cache(v))
	return f.(encodeState)
}

func (s *encodeState) cache(v any) encodeState {
	val, err := s.encode(v)
	if err != nil {
		s.setError(s.Name(), marshalError, err)
	}
	s.buf = val
	return *s
}

func (s *encodeState) encode(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)

	// If the value can be an interface, try asserting it in a Marshal interface.
	if rv.CanInterface() {
		if f, ok := s.IsMarshaller(rv); ok {
			return f()
		}
	}

	// If the value is a pointer, get the value pointed to by the pointer.
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}

	// If the value is not a struct, encode it as a simple type.
	if rv.Kind() != reflect.Struct {
		return s.encodeType(rv)
	}

	// The value is a struct, encode it as a struct type.
	return s.encodeStruct(rv, s.wrap)
}

// encodeStruct encodes a struct type into the encodeState buffer.
func (s *encodeState) encodeStruct(v reflect.Value, wrap bool) ([]byte, error) {
	var (
		buf      bytes.Buffer
		separate bool
	)

	t := v.Type()
	s.structName = t.Name()

	if wrap {
		buf.Write(s.structOpener)
	}

	// Scan v for fields to encode.
	for i := 0; i < t.NumField(); i++ {
		structField := t.Field(i)
		fieldValue := v.Field(i)

		s.fieldName = structField.Name
		s.context.fieldType = structField.Type

		fieldType := structField.Type
		if fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}

		if structField.Anonymous {
			// Ignore embedded fields of unexported non-struct types.
			if !structField.IsExported() && fieldType.Kind() != reflect.Struct {
				continue
			}

			// Do not ignore embedded fields of unexported struct types since they may have exported fields.
			if separate {
				buf.Write(s.valueSeparator)
			}

			if fieldValue.Kind() == reflect.Pointer {
				if fieldValue.IsNil() {
					continue
				}
				fieldValue = fieldValue.Elem()
			}

			val, err := s.encodeStruct(fieldValue, false)
			if err != nil {
				return nil, err
			}

			buf.Write(val)
			separate = s.separate
			continue
		} else if !structField.IsExported() {
			// Ignore unexported non-embedded fields.
			continue
		}

		tag, ok := structField.Tag.Lookup(s.Name())
		if ok {
			// Ignore the field if the tag has a skip fieldValue or if empty values can be omitted.
			if !fieldValue.IsValid() || s.Skip(tag) || s.Omitempty(tag) && isEmptyValue(fieldValue) {
				continue
			}
		}

		if separate {
			buf.Write(s.valueSeparator)
		}

		s.fieldName = structField.Name

		val, err := s.encodeType(fieldValue)
		if err != nil {
			return nil, err
		}

		if val, err = s.Encode(tag, structField.Name, val); err != nil {
			return nil, err
		}

		buf.Write(val)
		separate = s.separate
		continue
	}

	if wrap {
		buf.Write(s.structCloser)
	}

	return buf.Bytes(), nil
}

// encodeType encodes a simple type into the encodeState buffer.
func (s *encodeState) encodeType(v reflect.Value) ([]byte, error) {
	switch k := v.Kind(); k {
	case reflect.Bool:
		return []byte(strconv.FormatBool(v.Bool())), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return []byte(strconv.FormatInt(v.Int(), 10)), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return []byte(strconv.FormatUint(v.Uint(), 10)), nil
	case reflect.Float32, reflect.Float64:
		return []byte(strconv.FormatFloat(v.Float(), 'g', -1, bitSize(k))), nil
	//case reflect.Array: // TODO
	case reflect.Interface:
		if v.IsNil() {
			return nil, ErrNilInterface
		}
		return s.encode(v.Elem().Interface())
	//case reflect.Map: // TODO
	case reflect.Pointer:
		if v.IsNil() {
			v = reflect.New(v.Type().Elem())
		}
		return s.encodeType(v.Elem())
	//case reflect.Slice: // TODO
	case reflect.String:
		return []byte(v.String()), nil
	case reflect.Struct:
		return s.encode(v.Interface())
	default:
		return nil, ErrNotSupportType
	}
}
