package engine

import (
	"reflect"
)

type Default struct{}

func (d Default) Skip(string) bool {
	return false
}

func (d Default) Omitempty(string) bool {
	return false
}

func (d Default) IsMarshaller(reflect.Value) (func() ([]byte, error), bool) {
	return nil, false
}

func (d Default) IsUnmarshaler(reflect.Value) (func([]byte) error, bool) {
	return nil, false
}

func (d Default) f() {}
