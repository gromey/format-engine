package engine

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
)

const unmarshalError = "unmarshal data into"

// Unmarshal decodes the encoded data and stores the result in the value pointed to by v.
// If v is nil or not a pointer, Unmarshal returns a decoder error.
func (e engine) Unmarshal(data []byte, v any) error {
	if v == nil {
		return fmt.Errorf("%s: Unmarshal(nil)", e.Name())
	}

	s := e.newDecodeState()
	s = s.cachedDecode(data, v)

	return s.err
}

type decodeState struct {
	engine
	context
}

func (e engine) newDecodeState() decodeState {
	return decodeState{engine: e}
}

var decodeCache sync.Map // map[reflect.Type]decodeState

// cachedDecode uses a cache to avoid repeated work.
func (s *decodeState) cachedDecode(data []byte, v any) decodeState {
	if f, ok := decodeCache.Load(v); ok {
		return f.(decodeState)
	}
	f, _ := decodeCache.LoadOrStore(v, s.cache(data, v))
	return f.(decodeState)
}

func (s *decodeState) cache(data []byte, v any) decodeState {
	tmp := make([]byte, len(data))
	copy(tmp, data)
	if err := s.decode(tmp, v); err != nil {
		if errors.Is(err, ErrPointerToUnexported) || errors.Is(err, ErrInvalidFormat) {
			s.err = err
		} else {
			s.setError(s.Name(), unmarshalError, err)
		}
	}
	return *s
}

func (s *decodeState) decode(data []byte, v any) error {
	rv := reflect.ValueOf(v)

	// If the input value is not a pointer, return the error.
	if rv.Kind() != reflect.Pointer {
		return fmt.Errorf("%s: Unmarshal(non-pointer %s)", s.Name(), rv.Type())
	}

	// If the value has at least one method and can be an interface, try asserting it in an Unmarshal interface.
	if rv.Type().NumMethod() > 0 && rv.CanInterface() {
		if f, ok := s.IsUnmarshaler(rv); ok {
			return f(data)
		}
	}

	// If the input value is not a struct, decode it as a simple type.
	if rv = rv.Elem(); rv.Kind() != reflect.Struct {
		return s.decodeType(data, rv)
	}

	// The value is a struct, decode it as a struct type.
	if err := s.decodeStruct(data, rv, s.wrap); err != nil {
		return err
	}

	v = rv.Interface()

	return nil
}

// decodeStruct decodes a byte array into a struct type.
func (s *decodeState) decodeStruct(data []byte, v reflect.Value, unwrap bool) (err error) {
	var separate bool

	t := v.Type()
	s.structName = t.Name()

	if unwrap {
		if data, err = s.unwrap(data); err != nil {
			return
		}
	}

	// Scan v for fields to decode.
	for i := 0; i < t.NumField(); i++ {
		// If the data is over, stop decoding.
		if bytes.Trim(data, " ") == nil {
			return
		}

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
			if fieldValue.Kind() == reflect.Pointer {
				if fieldValue.IsNil() {
					return fmt.Errorf("%s: %w: %s", s.Name(), ErrPointerToUnexported, fieldValue.Type().Elem())
				}
				fieldValue = fieldValue.Elem()
			}

			if err = s.decodeStruct(data, fieldValue, false); err != nil {
				return
			}

			if s.separate {
				if data, err = s.removeSeparator(data); err != nil {
					return
				}
			}

			separate = s.separate
			continue
		} else if !structField.IsExported() {
			// Ignore unexported non-embedded fields.
			continue
		}

		tag, ok := structField.Tag.Lookup(s.Name())
		if ok {
			// Ignore the field if the tag has a skip value.
			if !fieldValue.IsValid() || s.Skip(tag) {
				continue
			}
		}

		if separate {
			if data, err = s.removeSeparator(data); err != nil {
				return
			}
		}

		var val []byte
		if val, err = s.Decode(tag, structField.Name, data); err != nil {
			return
		}

		if len(val) == 0 {
			continue
		}

		if err = s.decodeType(val, fieldValue); err != nil {
			return
		}

		separate = s.separate
	}

	return
}

// decodeType decodes a byte array into a simple type.
func (s *decodeState) decodeType(data []byte, v reflect.Value) (err error) {
	switch k := v.Kind(); k {
	case reflect.Bool:
		var b bool
		if b, err = strconv.ParseBool(string(data)); err != nil {
			return
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var i int64
		if i, err = strconv.ParseInt(string(data), 10, bitSize(k)); err != nil {
			return
		}
		v.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		var u uint64
		if u, err = strconv.ParseUint(string(data), 10, bitSize(k)); err != nil {
			return
		}
		v.SetUint(u)
	case reflect.Float32, reflect.Float64:
		var f float64
		if f, err = strconv.ParseFloat(string(data), bitSize(k)); err != nil {
			return
		}
		v.SetFloat(f)
	//case reflect.Array: // TODO
	case reflect.Interface:
		if !v.IsNil() {
			return s.decode(data, v.Elem().Interface())
		}
		return ErrNilInterface
	//case reflect.Map: // TODO
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()).Elem().Addr())
		}
		return s.decodeType(data, v.Elem())
	//case reflect.Slice: // TODO
	case reflect.String:
		v.SetString(string(data))
	case reflect.Struct:
		rv := reflect.New(v.Type())
		rv.Elem().Set(reflect.ValueOf(v.Interface()))
		if err = s.decode(data, rv.Interface()); err != nil {
			return
		}
		v.Set(rv.Elem())
	default:
		return ErrNotSupportType
	}
	return
}

func (s *decodeState) unwrap(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, s.structOpener) || !bytes.HasSuffix(data, s.structCloser) {
		return nil, fmt.Errorf("%s: %w", s.Name(), ErrInvalidFormat)
	}
	return data[len(s.structOpener) : len(data)-len(s.structCloser)], nil
}

func (s *decodeState) removeSeparator(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, s.valueSeparator) {
		return nil, fmt.Errorf("%s: %w", s.Name(), ErrInvalidFormat)
	}
	return data[len(s.valueSeparator):], nil
}
