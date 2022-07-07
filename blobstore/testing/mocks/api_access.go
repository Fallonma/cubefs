// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/cubefs/cubefs/blobstore/api/access (interfaces: API)

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	io "io"
	reflect "reflect"

	access "github.com/cubefs/cubefs/blobstore/api/access"
	gomock "github.com/golang/mock/gomock"
)

// MockAccessAPI is a mock of API interface.
type MockAccessAPI struct {
	ctrl     *gomock.Controller
	recorder *MockAccessAPIMockRecorder
}

// MockAccessAPIMockRecorder is the mock recorder for MockAccessAPI.
type MockAccessAPIMockRecorder struct {
	mock *MockAccessAPI
}

// NewMockAccessAPI creates a new mock instance.
func NewMockAccessAPI(ctrl *gomock.Controller) *MockAccessAPI {
	mock := &MockAccessAPI{ctrl: ctrl}
	mock.recorder = &MockAccessAPIMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAccessAPI) EXPECT() *MockAccessAPIMockRecorder {
	return m.recorder
}

// Delete mocks base method.
func (m *MockAccessAPI) Delete(arg0 context.Context, arg1 *access.DeleteArgs) ([]access.Location, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Delete", arg0, arg1)
	ret0, _ := ret[0].([]access.Location)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Delete indicates an expected call of Delete.
func (mr *MockAccessAPIMockRecorder) Delete(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Delete", reflect.TypeOf((*MockAccessAPI)(nil).Delete), arg0, arg1)
}

// Get mocks base method.
func (m *MockAccessAPI) Get(arg0 context.Context, arg1 *access.GetArgs) (io.ReadCloser, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", arg0, arg1)
	ret0, _ := ret[0].(io.ReadCloser)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Get indicates an expected call of Get.
func (mr *MockAccessAPIMockRecorder) Get(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockAccessAPI)(nil).Get), arg0, arg1)
}

// Put mocks base method.
func (m *MockAccessAPI) Put(arg0 context.Context, arg1 *access.PutArgs) (access.Location, access.HashSumMap, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Put", arg0, arg1)
	ret0, _ := ret[0].(access.Location)
	ret1, _ := ret[1].(access.HashSumMap)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// Put indicates an expected call of Put.
func (mr *MockAccessAPIMockRecorder) Put(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Put", reflect.TypeOf((*MockAccessAPI)(nil).Put), arg0, arg1)
}
