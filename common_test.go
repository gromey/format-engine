package engine

import (
	"errors"
	"math/bits"
	"reflect"
	"testing"
)

func equal(t *testing.T, exp, got interface{}) {
	if !reflect.DeepEqual(exp, got) {
		t.Fatalf("Not equal:\nexp: %v\ngot: %v", exp, got)
	}
}

func Test_bitSize(t *testing.T) {
	var tests = []struct {
		reflectKind reflect.Kind
		expect      int
	}{
		{
			reflectKind: reflect.Bool,
			expect:      0,
		},
		{
			reflectKind: reflect.Int8,
			expect:      8,
		},
		{
			reflectKind: reflect.Int16,
			expect:      16,
		},
		{
			reflectKind: reflect.Int32,
			expect:      32,
		},
		{
			reflectKind: reflect.Int64,
			expect:      64,
		},
		{
			reflectKind: reflect.Uint8,
			expect:      8,
		},
		{
			reflectKind: reflect.Uint16,
			expect:      16,
		},
		{
			reflectKind: reflect.Uint32,
			expect:      32,
		},
		{
			reflectKind: reflect.Uint64,
			expect:      64,
		},
		{
			reflectKind: reflect.Int,
			expect:      bits.UintSize,
		},
		{
			reflectKind: reflect.Uint,
			expect:      bits.UintSize,
		},
		{
			reflectKind: reflect.Uintptr,
			expect:      bits.UintSize,
		},
	}
	for _, tt := range tests {
		i := bitSize(tt.reflectKind)
		equal(t, tt.expect, i)
	}
}

func Test_isEmptyValue(t *testing.T) {
	var tests = []struct {
		value  any
		expect bool
	}{
		{
			value:  true,
			expect: false,
		},
		{
			value:  false,
			expect: true,
		},
		{
			value:  1,
			expect: false,
		},
		{
			value:  0,
			expect: true,
		},
		{
			value:  1.1,
			expect: false,
		},
		{
			value:  0.0,
			expect: true,
		},
		{
			value:  "a",
			expect: false,
		},
		{
			value:  "",
			expect: true,
		},
		{
			value:  &struct{}{},
			expect: false,
		},
		{
			value:  (*struct{})(nil),
			expect: true,
		},
		{
			value:  []int{1},
			expect: false,
		},
		{
			value:  []int{},
			expect: true,
		},
	}
	for _, tt := range tests {
		b := isEmptyValue(reflect.ValueOf(tt.value))
		equal(t, tt.expect, b)
	}
}

type empty struct{}

func Test_contextSetError(t *testing.T) {
	name := "tagName"
	str := "marshal/unmarshal"

	var tests = []struct {
		ctx    context[empty]
		expect error
	}{
		{
			ctx: context[empty]{
				structName: "structName",
				field: field[empty]{
					name: "fieldName",
					typ:  reflect.TypeOf(true),
				},
				err: ErrNotSupportType,
			},
			expect: errors.New("tagName: cannot marshal/unmarshal Go struct field structName.fieldName of type bool: cannot support type"),
		},
		{
			ctx: context[empty]{
				structName: "",
				field: field[empty]{
					name: "fieldName",
					typ:  reflect.TypeOf(true),
				},
				err: ErrNotSupportType,
			},
			expect: errors.New("tagName: cannot marshal/unmarshal Go value of type bool: cannot support type"),
		},
	}
	for _, tt := range tests {
		tt.ctx.setError(name, str, tt.ctx.err)
		if tt.expect != nil {
			equal(t, tt.expect.Error(), tt.ctx.err.Error())
		} else {
			equal(t, nil, tt.ctx.err)
		}
	}
}
