package engine

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
)

const marshalError = "encode data from"

// Marshal encodes the value v and returns the encoded data.
// If v is nil, Marshal returns an encoder error.
func (e *engine[T]) Marshal(v any) (out []byte, err error) {
	s := e.newEncodeState()
	defer encodeStatePool.Put(s)

	s.marshal(v)
	return s.Bytes(), s.err
}

type encodeState[T any] struct {
	*engine[T]
	context[T]
	*bytes.Buffer // accumulated output
	scratch       [64]byte
}

var encodeStatePool sync.Pool

func (e *engine[T]) newEncodeState() *encodeState[T] {
	if p := encodeStatePool.Get(); p != nil {
		s := p.(*encodeState[T])
		s.Reset()
		s.err = nil
		return s
	}

	return &encodeState[T]{engine: e, Buffer: new(bytes.Buffer)}
}

func (s *encodeState[T]) marshal(v any) {
	if err := s.reflectValue(reflect.ValueOf(v)); err != nil {
		if !errors.Is(err, errExist) {
			s.setError(s.Name(), marshalError, err)
		}
		s.Reset()
	}
}

func (s *encodeState[T]) reflectValue(v reflect.Value) error {
	return s.cache(v.Type())(s, v)
}

type encoderFunc[T any] func(*encodeState[T], reflect.Value) error

var encoderCache sync.Map // map[reflect.Type]encoderFunc[T]

// cache uses a cache to avoid repeated work.
func (s *encodeState[T]) cache(t reflect.Type) encoderFunc[T] {
	if c, ok := encoderCache.Load(t); ok {
		return c.(encoderFunc[T])
	}

	// To deal with recursive types, populate the map with an indirect func before we build it.
	// This type waits on the real func (f) to be ready and then calls it.
	// This indirect func is only used for recursive types.
	var (
		wg sync.WaitGroup
		f  encoderFunc[T]
	)
	wg.Add(1)
	c, loaded := encoderCache.LoadOrStore(t, encoderFunc[T](func(s *encodeState[T], v reflect.Value) error {
		wg.Wait()
		return f(s, v)
	}))
	if loaded {
		return c.(encoderFunc[T])
	}

	// Compute the real encoder and replace the indirect func with it.
	f, _ = s.typeCoders(t)
	wg.Done()
	encoderCache.Store(t, f)
	return f
}

func valueFromPtr(v reflect.Value) reflect.Value {
	if v.IsNil() {
		v = reflect.New(v.Type().Elem())
	}
	return v.Elem()
}

func (f *structFields[T]) encode(s *encodeState[T], v reflect.Value, wrap bool) (err error) {
	var sep bool

	s.structName = v.Type().Name()

	if wrap {
		s.Write(s.structOpener)
	}

	for _, s.field = range *f {
		rv := v.Field(s.field.index)

		// Ignore the field if empty values can be omitted.
		if s.field.omitEmpty && isEmptyValue(rv) {
			continue
		}

		if sep {
			s.Write(s.valueSeparator)
		}
		sep = s.separate

		if s.field.embedded != nil {
			if err = s.field.embedded.encode(s, valueFromPtr(rv), false); err != nil {
				return
			}
			continue
		}

		if err = s.field.encoder(s, rv); err != nil {
			return
		}
	}

	if wrap {
		s.Write(s.structCloser)
	}

	return
}

func marshallerEncoder[T any](s *encodeState[T], v reflect.Value) error {
	tmp := reflect.ValueOf(v.Interface())
	v = reflect.New(v.Type())
	v.Elem().Set(tmp)

	f, ok := s.IsMarshaller(v)
	if !ok {
		return nil
	}

	p, err := f()
	if err != nil {
		return err
	}

	return s.Encode(s.field.name, s.field.meta, p, s.Buffer)
}

func boolEncoder[T any](s *encodeState[T], v reflect.Value) error {
	return s.Encode(s.field.name, s.field.meta, strconv.AppendBool(s.scratch[:0], v.Bool()), s.Buffer)
}

func intEncoder[T any](s *encodeState[T], v reflect.Value) error {
	return s.Encode(s.field.name, s.field.meta, strconv.AppendInt(s.scratch[:0], v.Int(), 10), s.Buffer)
}

func uintEncoder[T any](s *encodeState[T], v reflect.Value) error {
	return s.Encode(s.field.name, s.field.meta, strconv.AppendUint(s.scratch[:0], v.Uint(), 10), s.Buffer)
}

func floatEncoder[T any](s *encodeState[T], v reflect.Value) error {
	return s.Encode(s.field.name, s.field.meta, strconv.AppendFloat(s.scratch[:0], v.Float(), 'g', -1, bitSize(v.Kind())), s.Buffer)
}

func interfaceEncoder[T any](s *encodeState[T], v reflect.Value) error {
	if v.IsNil() {
		s.err = ErrNilInterface
		return errExist
	}
	return s.reflectValue(v.Elem())
}

func pointerEncoder[T any](s *encodeState[T], v reflect.Value) error {
	return s.reflectValue(valueFromPtr(v))
}

func bytesEncoder[T any](s *encodeState[T], v reflect.Value) error {
	return s.Encode(s.field.name, s.field.meta, v.Bytes(), s.Buffer)
}

func sliceEncoder[T any](s *encodeState[T], v reflect.Value) error {
	return nil // TODO
}

func stringEncoder[T any](s *encodeState[T], v reflect.Value) error {
	return s.Encode(s.field.name, s.field.meta, append(s.scratch[:0], v.String()...), s.Buffer)
}

func structEncoder[T any](s *encodeState[T], v reflect.Value) error {
	f := s.cachedFields(v.Type())
	return f.encode(s, reflect.ValueOf(v.Interface()), s.wrap)
}

func unsupportedTypeEncoder[T any](s *encodeState[T], _ reflect.Value) error {
	s.err = ErrNotSupportType
	return errExist
}

func invalidTagEncoder[T any](tag string, err error) encoderFunc[T] {
	return func(s *encodeState[T], _ reflect.Value) error {
		s.err = fmt.Errorf("%s: tag %s of struct field %s.%s: %w", s.Name(), tag, s.structName, s.field.name, err)
		return nil
	}
}
