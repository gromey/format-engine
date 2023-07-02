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
func (e *engine) Unmarshal(data []byte, v any) error {
	s := e.newDecodeState()
	defer decodeStatePool.Put(s)

	s.buf = make([]byte, len(data))
	copy(s.buf, data)

	s.unmarshal(v)
	return s.err
}

type decodeState struct {
	buf []byte
	tmp []byte
	context
	*engine
}

var decodeStatePool sync.Pool

func (e *engine) newDecodeState() *decodeState {
	if p := decodeStatePool.Get(); p != nil {
		s := p.(*decodeState)
		s.err = nil
		return s
	}

	return &decodeState{engine: e}
}

func (s *decodeState) unmarshal(v any) {
	if err := s.reflectValue(reflect.ValueOf(v)); err != nil {
		if errors.Is(err, ErrPointerToUnexported) || errors.Is(err, ErrInvalidFormat) {
			s.err = err
		} else {
			s.setError(s.Name(), unmarshalError, err)
		}
	}
}

func (s *decodeState) reflectValue(v reflect.Value) error {
	return s.cache(v.Type())(s, v)
}

type decoderFunc func(*decodeState, reflect.Value) error

var decoderCache sync.Map // map[reflect.Type]decoderFunc

// cache uses a cache to avoid repeated work.
func (s *decodeState) cache(t reflect.Type) decoderFunc {
	if c, ok := decoderCache.Load(t); ok {
		return c.(decoderFunc)
	}

	// To deal with recursive types, populate the map with an indirect func before we build it.
	// This type waits on the real func (f) to be ready and then calls it.
	// This indirect func is only used for recursive types.
	var (
		wg sync.WaitGroup
		f  decoderFunc
	)
	wg.Add(1)
	c, loaded := decoderCache.LoadOrStore(t, decoderFunc(func(s *decodeState, v reflect.Value) error {
		wg.Wait()
		return f(s, v)
	}))
	if loaded {
		return c.(decoderFunc)
	}

	// Compute the real encoder and replace the indirect func with it.
	_, f = s.typeCoders(t)
	wg.Done()
	decoderCache.Store(t, f)
	return f
}

func (s *decodeState) removePrefixBytes(b []byte) error {
	if !bytes.HasPrefix(s.buf, b) {
		return fmt.Errorf("%s: %w", s.Name(), ErrInvalidFormat)
	}
	s.buf = s.buf[len(b):]
	return nil
}

func (f *structFields) decode(s *decodeState, v reflect.Value, unwrap bool) (err error) {
	var sep bool

	s.structName = v.Type().Name()

	if unwrap {
		if err = s.removePrefixBytes(s.structOpener); err != nil {
			return
		}
	}

	for _, s.field = range *f {
		if s.buf = bytes.Trim(s.buf, " "); s.buf == nil || unwrap && bytes.HasPrefix(s.buf, s.structCloser) {
			break
		}

		if sep {
			if err = s.removePrefixBytes(s.valueSeparator); err != nil {
				return
			}
		}
		sep = s.removeSeparator

		rv := v.Field(s.field.index)

		if s.field.embedded != nil {
			if rv.Kind() == reflect.Pointer {
				if rv.IsNil() {
					return fmt.Errorf("%s: %w: %s", s.Name(), ErrPointerToUnexported, rv.Type().Elem())
				}
				rv = rv.Elem()
			}

			if err = s.field.embedded.decode(s, rv, false); err != nil {
				return
			}
			continue
		}

		if s.tmp, err = s.Decode(s.field.tag, s.field.name, s.buf); err != nil {
			return err
		}

		if s.tmp == nil {
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

func boolDecoder(s *decodeState, v reflect.Value) error {
	r, err := strconv.ParseBool(string(s.tmp))
	if err != nil {
		return err
	}
	v.SetBool(r)
	return nil
}

func intDecoder(s *decodeState, v reflect.Value) error {
	r, err := strconv.ParseInt(string(s.tmp), 10, bitSize(v.Kind()))
	if err != nil {
		return err
	}
	v.SetInt(r)
	return nil
}

func uintDecoder(s *decodeState, v reflect.Value) error {
	r, err := strconv.ParseUint(string(s.tmp), 10, bitSize(v.Kind()))
	if err != nil {
		return err
	}
	v.SetUint(r)
	return nil
}

func floatDecoder(s *decodeState, v reflect.Value) error {
	r, err := strconv.ParseFloat(string(s.tmp), bitSize(v.Kind()))
	if err != nil {
		return err
	}
	v.SetFloat(r)
	return nil
}

func interfaceDecoder(s *decodeState, v reflect.Value) error {
	if v.IsNil() {
		return ErrNilInterface
	}
	return s.reflectValue(v.Elem())
}

func pointerDecoder(s *decodeState, v reflect.Value) error {
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

func bytesDecoder(s *decodeState, v reflect.Value) error {
	v.SetBytes(s.tmp)
	return nil
}

func sliceDecoder(s *decodeState, v reflect.Value) error {
	return nil // TODO
}

func stringDecoder(s *decodeState, v reflect.Value) error {
	v.SetString(string(s.tmp))
	return nil
}

func structDecoder(s *decodeState, v reflect.Value) error {
	f := s.cachedFields(v.Type())
	return f.decode(s, v, s.wrap)
}

func unsupportedTypeDecoder(*decodeState, reflect.Value) error {
	return ErrNotSupportType
}

func unmarshalerDecoder(s *decodeState, v reflect.Value) error {
	var rv reflect.Value
	if v.Kind() != reflect.Pointer {
		rv = reflect.New(v.Type())
	}

	f, ok := s.IsUnmarshaler(rv)
	if !ok {
		return nil
	}

	if err := f(s.tmp); err != nil {
		return err
	}

	v.Set(rv.Elem())
	return nil
}
