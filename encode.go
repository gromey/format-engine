package engine

import (
	"bytes"
	"reflect"
	"strconv"
	"sync"
)

const marshalError = "encode data from"

// Marshal encodes the value v and returns the encoded data.
// If v is nil, Marshal returns an encoder error.
func (e *engine) Marshal(v any) ([]byte, error) {
	s := e.newEncodeState()
	defer encodeStatePool.Put(s)

	s.marshal(v)
	return s.Bytes(), s.err
}

type encodeState struct {
	bytes.Buffer // accumulated output
	scratch      [64]byte
	context
	*engine
}

var encodeStatePool sync.Pool

func (e *engine) newEncodeState() *encodeState {
	if p := encodeStatePool.Get(); p != nil {
		s := p.(*encodeState)
		s.Reset()
		s.err = nil
		return s
	}

	return &encodeState{engine: e}
}

func (s *encodeState) marshal(v any) {
	if err := s.reflectValue(reflect.ValueOf(v)); err != nil {
		s.setError(s.Name(), marshalError, err)
		s.Reset()
	}
}

func (s *encodeState) reflectValue(v reflect.Value) error {
	return s.cache(v.Type())(s, v)
}

type encoderFunc func(*encodeState, reflect.Value) error

var encoderCache sync.Map // map[reflect.Type]encoderFunc

// cache uses a cache to avoid repeated work.
func (s *encodeState) cache(t reflect.Type) encoderFunc {
	if c, ok := encoderCache.Load(t); ok {
		return c.(encoderFunc)
	}

	// To deal with recursive types, populate the map with an indirect func before we build it.
	// This type waits on the real func (f) to be ready and then calls it.
	// This indirect func is only used for recursive types.
	var (
		wg sync.WaitGroup
		f  encoderFunc
	)
	wg.Add(1)
	c, loaded := encoderCache.LoadOrStore(t, encoderFunc(func(s *encodeState, v reflect.Value) error {
		wg.Wait()
		return f(s, v)
	}))
	if loaded {
		return c.(encoderFunc)
	}

	// Compute the real encoder and replace the indirect func with it.
	f, _ = s.typeCoders(t)
	wg.Done()
	encoderCache.Store(t, f)
	return f
}

func (s *encodeState) encode(data []byte) error {
	p, err := s.Encode(s.field.tag, s.field.name, data)
	if err != nil {
		return err
	}
	s.Write(p)
	return nil
}

func (f *structFields) encode(s *encodeState, v reflect.Value, wrap bool) (err error) {
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

func boolEncoder(s *encodeState, v reflect.Value) error {
	return s.encode(strconv.AppendBool(s.scratch[:0], v.Bool()))
}

func intEncoder(s *encodeState, v reflect.Value) error {
	return s.encode(strconv.AppendInt(s.scratch[:0], v.Int(), 10))
}

func uintEncoder(s *encodeState, v reflect.Value) error {
	return s.encode(strconv.AppendUint(s.scratch[:0], v.Uint(), 10))
}

func floatEncoder(s *encodeState, v reflect.Value) error {
	return s.encode(strconv.AppendFloat(s.scratch[:0], v.Float(), 'g', -1, bitSize(v.Kind())))
}

func interfaceEncoder(s *encodeState, v reflect.Value) error {
	if v.IsNil() {
		return ErrNilInterface
	}
	return s.reflectValue(v.Elem())
}

func pointerEncoder(s *encodeState, v reflect.Value) error {
	return s.reflectValue(valueFromPtr(v))
}

func bytesEncoder(s *encodeState, v reflect.Value) error {
	return s.encode(v.Bytes())
}

func sliceEncoder(s *encodeState, v reflect.Value) error {
	return nil // TODO
}

func stringEncoder(s *encodeState, v reflect.Value) error {
	return s.encode([]byte(v.String()))
}

func structEncoder(s *encodeState, v reflect.Value) error {
	f := s.cachedFields(v.Type())
	return f.encode(s, reflect.ValueOf(v.Interface()), s.wrap)
}

func unsupportedTypeEncoder(*encodeState, reflect.Value) error {
	return ErrNotSupportType
}

func marshallerEncoder(s *encodeState, v reflect.Value) error {
	// If we have a non-pointer value whose type implements Marshaller with a value receiver,
	// then we're better off taking the address of the value - otherwise we end up with an
	// allocation as we cast the value to an interface.
	if v.Kind() != reflect.Pointer {
		tmp := reflect.ValueOf(v.Interface())
		v = reflect.New(v.Type())
		v.Elem().Set(tmp)
	}
	f, ok := s.IsMarshaller(v)
	if !ok {
		return nil
	}
	p, err := f()
	if err != nil {
		return err
	}
	return s.encode(p)
}

func valueFromPtr(v reflect.Value) reflect.Value {
	if v.Kind() != reflect.Pointer {
		return v
	}
	if v.IsNil() {
		v = reflect.New(v.Type().Elem())
	}
	return v.Elem()
}
