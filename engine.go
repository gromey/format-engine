package engine

import (
	"io"
	"reflect"
)

// Engine represents the main functions that the package implements.
type Engine interface {
	// Marshal encodes the value v and returns the encoded data.
	Marshal(v any) ([]byte, error)
	// Unmarshal decodes the encoded data and stores the result in the value pointed to by v.
	Unmarshal(data []byte, v any) error
}

type Writer interface {
	io.Writer
	io.ByteWriter
}

// Tag describes what functions an entity should implement to use when creating a new Engine entity.
// The entity must include an engine.Default that implements following default methods:
//   - Skip;
//   - Parse.
//
// So it may not implement these methods.
type Tag[T any] interface {
	// Name returns the name of the tag. It's a mandatory function.
	Name() string
	// Skip returns a flag indicating that the field should be ignored.
	Skip(tagValue string) bool
	// Parse gets a tagValue string, parses the tagValue into tag *T,
	// returns a flag indicating that the field is skipped if it's empty,
	// and if parsing fails, it returns an error.
	Parse(tagValue string, tag *T) (bool, error)
	// Encode takes encoded data and performs secondary encoding.
	// It's a mandatory function.
	Encode(fieldName string, tag *T, in []byte, out Writer) error
	// Decode takes the raw encoded data and performs a primary decode.
	// It's a mandatory function.
	Decode(fieldName string, tag *T, in []byte, out Writer) error
	// IsMarshaller attempts to cast the value to a Marshaller interface,
	// if so, returns a marshal function.
	IsMarshaller(v reflect.Value) (func() ([]byte, error), bool)
	// IsUnmarshaler attempts to cast the value to an Unmarshaler interface,
	// if so, returns an unmarshal function.
	IsUnmarshaler(v reflect.Value) (func([]byte) error, bool)

	f()
}

type Config struct {
	// StructOpener a byte array that denotes the beginning of a structure.
	// Will be automatically added when encoding.
	StructOpener []byte
	// StructCloser a byte array that denotes the end of a structure.
	// Will be automatically added when encoding.
	StructCloser []byte
	// UnwrapWhenDecoding this flag tells the library whether to remove the StructOpener and StructCloser bytes of a structure.
	UnwrapWhenDecoding bool
	// ValueSeparator a byte array separating values.
	// Will be automatically added when encoding.
	ValueSeparator []byte
	// RemoveSeparatorWhenDecoding this flag tells the library whether to remove the ValueSeparator.
	RemoveSeparatorWhenDecoding bool
	// Marshaller is used to check if a type implements a type of the Marshaller interface.
	Marshaller reflect.Type
	// Unmarshaler is used to check if a type implements a type of the Unmarshaler interface.
	Unmarshaler reflect.Type
}

type engine[T any] struct {
	Tag[T]
	wrap, separate, removeSeparator            bool
	structOpener, structCloser, valueSeparator []byte
	marshaller, unmarshaler                    reflect.Type
}

// New returns a new entity that implements the Engine interface.
func New[T any](tag Tag[T], cfg Config) Engine {
	return &engine[T]{
		Tag:             tag,
		wrap:            (len(cfg.StructOpener) != 0 || len(cfg.StructCloser) != 0) && cfg.UnwrapWhenDecoding,
		separate:        len(cfg.ValueSeparator) != 0,
		removeSeparator: len(cfg.ValueSeparator) != 0 && cfg.RemoveSeparatorWhenDecoding,
		structOpener:    cfg.StructOpener,
		structCloser:    cfg.StructCloser,
		valueSeparator:  cfg.ValueSeparator,
		marshaller:      cfg.Marshaller,
		unmarshaler:     cfg.Unmarshaler,
	}
}
