package engine

import (
	"reflect"
)

// Engine represents the main functions that the package implements.
type Engine interface {
	// Marshal encodes the value v and returns the encoded data.
	Marshal(v any) ([]byte, error)
	// Unmarshal decodes the encoded data and stores the result in the value pointed to by v.
	Unmarshal(data []byte, v any) error
}

// Tag describes what functions an entity should implement to use when creating a new Engine entity.
// The entity must include an engine.Default that implements following methods:
//   - Skip;
//   - Omitempty;
//   - IsMarshaller;
//   - IsUnmarshaler.
//
// So it may not implement these methods.
type Tag interface {
	// Name returns the name of the tag. It's a mandatory function.
	Name() string
	// Encode takes encoded data and performs secondary encoding.
	// It's a mandatory function.
	Encode(tagValue, fieldName string, data []byte) ([]byte, error)
	// Decode takes the raw encoded data and performs a primary decode.
	// It's a mandatory function.
	Decode(tagValue, fieldName string, data []byte) ([]byte, error)
	// Skip returns a flag indicating that the field should be ignored.
	Skip(tagValue string) bool
	// Omitempty returns a flag indicating that the field is skipped if empty.
	Omitempty(tagValue string) bool
	// IsMarshaller attempts to cast the value to a Marshaller interface,
	// if so, returns a marshal function.
	IsMarshaller(rv reflect.Value) (func() ([]byte, error), bool)
	// IsUnmarshaler attempts to cast the value to an Unmarshaler interface,
	// if so, returns an unmarshal function.
	IsUnmarshaler(rv reflect.Value) (func([]byte) error, bool)
	f()
}

type engine struct {
	Tag
	wrap, separate                             bool
	structOpener, structCloser, valueSeparator []byte
}

type Config struct {
	StructOpener                []byte
	StructCloser                []byte
	UnwrapWhenDecoding          bool
	ValueSeparator              []byte
	RemoveSeparatorWhenDecoding bool
}

// New returns a new entity that implements the Engine interface.
func New(tag Tag, cfg Config) Engine {
	return &engine{
		Tag:            tag,
		wrap:           (len(cfg.StructOpener) != 0 || len(cfg.StructCloser) != 0) && cfg.UnwrapWhenDecoding,
		separate:       len(cfg.ValueSeparator) != 0 && cfg.RemoveSeparatorWhenDecoding,
		structOpener:   cfg.StructOpener,
		structCloser:   cfg.StructCloser,
		valueSeparator: cfg.ValueSeparator,
	}
}
