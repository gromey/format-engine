package engine

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
)

const unmarshalError = "decode data into"

// Unmarshal decodes the encoded data and stores the result in the value pointed to by v.
// If v is nil or not a pointer, Unmarshal returns a decoder error.
func (e *engine[T]) Unmarshal(data []byte, v any) (err error) {
	s := e.newDecodeState()
	defer decodeStatePool.Put(s)

	s.data = make([]byte, len(data))
	copy(s.data, data)

	s.unmarshal(v)
	return s.err
}

type decodeState[T any] struct {
	*engine[T]
	context[T]
	*bytes.Buffer
	data []byte // copy of input
}

var decodeStatePool sync.Pool

func (e *engine[T]) newDecodeState() *decodeState[T] {
	if p := decodeStatePool.Get(); p != nil {
		s := p.(*decodeState[T])
		s.err = nil
		return s
	}

	return &decodeState[T]{engine: e, Buffer: new(bytes.Buffer)}
}

func (s *decodeState[T]) unmarshal(v any) {
	if err := s.reflectValue(reflect.ValueOf(v)); err != nil {
		if !errors.Is(err, errExist) {
			s.setError(s.Name(), unmarshalError, err)
		}
	}
}

func (s *decodeState[T]) reflectValue(v reflect.Value) error {
	return s.cache(v.Type())(s, v)
}

type decoderFunc[T any] func(*decodeState[T], reflect.Value) error

var decoderCache sync.Map // map[reflect.Type]decoderFunc[T]

// cache uses a cache to avoid repeated work.
func (s *decodeState[T]) cache(t reflect.Type) decoderFunc[T] {
	if c, ok := decoderCache.Load(t); ok {
		return c.(decoderFunc[T])
	}

	// To deal with recursive types, populate the map with an indirect func before we build it.
	// This type waits on the real func (f) to be ready and then calls it.
	// This indirect func is only used for recursive types.
	var (
		wg sync.WaitGroup
		f  decoderFunc[T]
	)
	wg.Add(1)
	c, loaded := decoderCache.LoadOrStore(t, decoderFunc[T](func(s *decodeState[T], v reflect.Value) error {
		wg.Wait()
		return f(s, v)
	}))
	if loaded {
		return c.(decoderFunc[T])
	}

	// Compute the real encoder and replace the indirect func with it.
	_, f = s.typeCoders(t)
	wg.Done()
	decoderCache.Store(t, f)
	return f
}

func (s *decodeState[T]) removePrefixBytes(b []byte) error {
	if !bytes.HasPrefix(s.data, b) {
		s.err = fmt.Errorf("%s: %w", s.Name(), ErrInvalidFormat)
		return errExist
	}
	s.data = s.data[len(b):]
	return nil
}

func (f *structFields[T]) decode(s *decodeState[T], v reflect.Value, unwrap bool) (err error) {
	var sep bool

	s.structName = v.Type().Name()

	if unwrap {
		if err = s.removePrefixBytes(s.structOpener); err != nil {
			return
		}
	}

	for _, s.field = range *f {
		if s.data = bytes.TrimSpace(s.data); s.data == nil || unwrap && bytes.HasPrefix(s.data, s.structCloser) {
			break
		}

		if sep {
			if err = s.removePrefixBytes(s.valueSeparator); err != nil {
				return
			}
		}
		sep = s.removeSeparator

		s.Reset()
		rv := v.Field(s.field.index)

		if s.field.embedded != nil {
			if rv.Kind() == reflect.Pointer {
				if rv.IsNil() {
					s.err = fmt.Errorf("%s: %w: %s", s.Name(), ErrPointerToUnexported, rv.Type().Elem())
					return errExist
				}
				rv = rv.Elem()
			}

			if err = s.field.embedded.decode(s, rv, false); err != nil {
				return
			}
			continue
		}

		if err = s.Decode(s.field.name, s.field.meta, s.data, s); err != nil {
			return
		}

		if s.Len() == 0 {
			continue
		}

		if err = s.field.decoder(s, rv); err != nil {
			return
		}
	}

	if unwrap {
		if err = s.removePrefixBytes(s.structCloser); err != nil {
			return
		}
	}

	return
}

func unmarshalerDecoder[T any](s *decodeState[T], v reflect.Value) error {
	rv := reflect.New(v.Type())

	f, ok := s.IsUnmarshaler(rv)
	if !ok {
		return nil
	}

	if err := f(s.Bytes()); err != nil {
		return err
	}

	v.Set(rv.Elem())
	return nil
}

func boolDecoder[T any](s *decodeState[T], v reflect.Value) error {
	r, err := strconv.ParseBool(s.String())
	v.SetBool(r)
	return err
}

func intDecoder[T any](s *decodeState[T], v reflect.Value) error {
	r, err := strconv.ParseInt(s.String(), 10, bitSize(v.Kind()))
	v.SetInt(r)
	return err
}

func uintDecoder[T any](s *decodeState[T], v reflect.Value) error {
	r, err := strconv.ParseUint(s.String(), 10, bitSize(v.Kind()))
	v.SetUint(r)
	return err
}

func floatDecoder[T any](s *decodeState[T], v reflect.Value) error {
	r, err := strconv.ParseFloat(s.String(), bitSize(v.Kind()))
	v.SetFloat(r)
	return err
}

func interfaceDecoder[T any](s *decodeState[T], v reflect.Value) error {
	if v.IsNil() {
		s.err = ErrNilInterface
		return errExist
	}
	return s.reflectValue(v.Elem())
}

func pointerDecoder[T any](s *decodeState[T], v reflect.Value) error {
	if v.IsNil() {
		rv := reflect.New(v.Type().Elem())
		if err := s.reflectValue(rv.Elem()); err != nil {
			return err
		}
		if !isEmptyValue(rv.Elem()) {
			v.Set(rv)
		}
		return nil
	}
	return s.reflectValue(v.Elem())
}

func bytesDecoder[T any](s *decodeState[T], v reflect.Value) error {
	v.SetBytes(s.Bytes())
	return nil
}

func sliceDecoder[T any](s *decodeState[T], v reflect.Value) error {
	return nil // TODO
}

func stringDecoder[T any](s *decodeState[T], v reflect.Value) error {
	v.SetString(s.String())
	return nil
}

func structDecoder[T any](s *decodeState[T], v reflect.Value) error {
	f := s.cachedFields(v.Type())
	return f.decode(s, v, s.wrap)
}

func unsupportedTypeDecoder[T any](s *decodeState[T], _ reflect.Value) error {
	s.err = ErrNotSupportType
	return errExist
}

func invalidTagDecoder[T any](tag string, err error) decoderFunc[T] {
	return func(s *decodeState[T], _ reflect.Value) error {
		s.err = fmt.Errorf("%s: tag %s of struct field %s.%s: %w", s.Name(), tag, s.structName, s.field.name, err)
		return errExist
	}
}
