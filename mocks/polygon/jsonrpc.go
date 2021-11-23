// Code generated by mockery v2.9.4. DO NOT EDIT.

package polygon

import (
	context "context"

	mock "github.com/stretchr/testify/mock"

	rpc "github.com/ethereum/go-ethereum/rpc"
)

// JSONRPC is an autogenerated mock type for the JSONRPC type
type JSONRPC struct {
	mock.Mock
}

// BatchCallContext provides a mock function with given fields: ctx, b
func (_m *JSONRPC) BatchCallContext(ctx context.Context, b []rpc.BatchElem) error {
	ret := _m.Called(ctx, b)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, []rpc.BatchElem) error); ok {
		r0 = rf(ctx, b)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// CallContext provides a mock function with given fields: ctx, result, method, args
func (_m *JSONRPC) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	var _ca []interface{}
	_ca = append(_ca, ctx, result, method)
	_ca = append(_ca, args...)
	ret := _m.Called(_ca...)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, interface{}, string, ...interface{}) error); ok {
		r0 = rf(ctx, result, method, args...)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Close provides a mock function with given fields:
func (_m *JSONRPC) Close() {
	_m.Called()
}
