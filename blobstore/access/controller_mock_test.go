// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/cubefs/cubefs/blobstore/access/controller (interfaces: ClusterController,ServiceController,VolumeGetter)

// Package access is a generated GoMock package.
package access

import (
	context "context"
	reflect "reflect"

	controller "github.com/cubefs/cubefs/blobstore/access/controller"
	clustermgr "github.com/cubefs/cubefs/blobstore/api/clustermgr"
	proto "github.com/cubefs/cubefs/blobstore/common/proto"
	gomock "github.com/golang/mock/gomock"
)

// MockClusterController is a mock of ClusterController interface.
type MockClusterController struct {
	ctrl     *gomock.Controller
	recorder *MockClusterControllerMockRecorder
}

// MockClusterControllerMockRecorder is the mock recorder for MockClusterController.
type MockClusterControllerMockRecorder struct {
	mock *MockClusterController
}

// NewMockClusterController creates a new mock instance.
func NewMockClusterController(ctrl *gomock.Controller) *MockClusterController {
	mock := &MockClusterController{ctrl: ctrl}
	mock.recorder = &MockClusterControllerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockClusterController) EXPECT() *MockClusterControllerMockRecorder {
	return m.recorder
}

// All mocks base method.
func (m *MockClusterController) All() []*clustermgr.ClusterInfo {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "All")
	ret0, _ := ret[0].([]*clustermgr.ClusterInfo)
	return ret0
}

// All indicates an expected call of All.
func (mr *MockClusterControllerMockRecorder) All() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "All", reflect.TypeOf((*MockClusterController)(nil).All))
}

// ChangeChooseAlg mocks base method.
func (m *MockClusterController) ChangeChooseAlg(arg0 controller.AlgChoose) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ChangeChooseAlg", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// ChangeChooseAlg indicates an expected call of ChangeChooseAlg.
func (mr *MockClusterControllerMockRecorder) ChangeChooseAlg(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ChangeChooseAlg", reflect.TypeOf((*MockClusterController)(nil).ChangeChooseAlg), arg0)
}

// ChooseOne mocks base method.
func (m *MockClusterController) ChooseOne() (*clustermgr.ClusterInfo, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ChooseOne")
	ret0, _ := ret[0].(*clustermgr.ClusterInfo)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ChooseOne indicates an expected call of ChooseOne.
func (mr *MockClusterControllerMockRecorder) ChooseOne() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ChooseOne", reflect.TypeOf((*MockClusterController)(nil).ChooseOne))
}

// GetConfig mocks base method.
func (m *MockClusterController) GetConfig(arg0 context.Context, arg1 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetConfig", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetConfig indicates an expected call of GetConfig.
func (mr *MockClusterControllerMockRecorder) GetConfig(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetConfig", reflect.TypeOf((*MockClusterController)(nil).GetConfig), arg0, arg1)
}

// GetServiceController mocks base method.
func (m *MockClusterController) GetServiceController(arg0 proto.ClusterID) (controller.ServiceController, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetServiceController", arg0)
	ret0, _ := ret[0].(controller.ServiceController)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetServiceController indicates an expected call of GetServiceController.
func (mr *MockClusterControllerMockRecorder) GetServiceController(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetServiceController", reflect.TypeOf((*MockClusterController)(nil).GetServiceController), arg0)
}

// GetVolumeAllocator mocks base method.
func (m *MockClusterController) GetVolumeAllocator(arg0 proto.ClusterID) (controller.VolumeMgr, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetVolumeAllocator", arg0)
	ret0, _ := ret[0].(controller.VolumeMgr)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetVolumeAllocator indicates an expected call of GetVolumeAllocator.
func (mr *MockClusterControllerMockRecorder) GetVolumeAllocator(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetVolumeAllocator", reflect.TypeOf((*MockClusterController)(nil).GetVolumeAllocator), arg0)
}

// GetVolumeGetter mocks base method.
func (m *MockClusterController) GetVolumeGetter(arg0 proto.ClusterID) (controller.VolumeGetter, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetVolumeGetter", arg0)
	ret0, _ := ret[0].(controller.VolumeGetter)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetVolumeGetter indicates an expected call of GetVolumeGetter.
func (mr *MockClusterControllerMockRecorder) GetVolumeGetter(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetVolumeGetter", reflect.TypeOf((*MockClusterController)(nil).GetVolumeGetter), arg0)
}

// Region mocks base method.
func (m *MockClusterController) Region() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Region")
	ret0, _ := ret[0].(string)
	return ret0
}

// Region indicates an expected call of Region.
func (mr *MockClusterControllerMockRecorder) Region() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Region", reflect.TypeOf((*MockClusterController)(nil).Region))
}

// MockServiceController is a mock of ServiceController interface.
type MockServiceController struct {
	ctrl     *gomock.Controller
	recorder *MockServiceControllerMockRecorder
}

// MockServiceControllerMockRecorder is the mock recorder for MockServiceController.
type MockServiceControllerMockRecorder struct {
	mock *MockServiceController
}

// NewMockServiceController creates a new mock instance.
func NewMockServiceController(ctrl *gomock.Controller) *MockServiceController {
	mock := &MockServiceController{ctrl: ctrl}
	mock.recorder = &MockServiceControllerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockServiceController) EXPECT() *MockServiceControllerMockRecorder {
	return m.recorder
}

// GetDiskHost mocks base method.
func (m *MockServiceController) GetDiskHost(arg0 context.Context, arg1 proto.DiskID) (*controller.HostIDC, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetDiskHost", arg0, arg1)
	ret0, _ := ret[0].(*controller.HostIDC)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetDiskHost indicates an expected call of GetDiskHost.
func (mr *MockServiceControllerMockRecorder) GetDiskHost(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetDiskHost", reflect.TypeOf((*MockServiceController)(nil).GetDiskHost), arg0, arg1)
}

// GetServiceHost mocks base method.
func (m *MockServiceController) GetServiceHost(arg0 context.Context, arg1 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetServiceHost", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetServiceHost indicates an expected call of GetServiceHost.
func (mr *MockServiceControllerMockRecorder) GetServiceHost(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetServiceHost", reflect.TypeOf((*MockServiceController)(nil).GetServiceHost), arg0, arg1)
}

// GetServiceHosts mocks base method.
func (m *MockServiceController) GetServiceHosts(arg0 context.Context, arg1 string) ([]string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetServiceHosts", arg0, arg1)
	ret0, _ := ret[0].([]string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetServiceHosts indicates an expected call of GetServiceHosts.
func (mr *MockServiceControllerMockRecorder) GetServiceHosts(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetServiceHosts", reflect.TypeOf((*MockServiceController)(nil).GetServiceHosts), arg0, arg1)
}

// PunishDisk mocks base method.
func (m *MockServiceController) PunishDisk(arg0 context.Context, arg1 proto.DiskID, arg2 int) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "PunishDisk", arg0, arg1, arg2)
}

// PunishDisk indicates an expected call of PunishDisk.
func (mr *MockServiceControllerMockRecorder) PunishDisk(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PunishDisk", reflect.TypeOf((*MockServiceController)(nil).PunishDisk), arg0, arg1, arg2)
}

// PunishDiskWithThreshold mocks base method.
func (m *MockServiceController) PunishDiskWithThreshold(arg0 context.Context, arg1 proto.DiskID, arg2 int) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "PunishDiskWithThreshold", arg0, arg1, arg2)
}

// PunishDiskWithThreshold indicates an expected call of PunishDiskWithThreshold.
func (mr *MockServiceControllerMockRecorder) PunishDiskWithThreshold(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PunishDiskWithThreshold", reflect.TypeOf((*MockServiceController)(nil).PunishDiskWithThreshold), arg0, arg1, arg2)
}

// PunishService mocks base method.
func (m *MockServiceController) PunishService(arg0 context.Context, arg1, arg2 string, arg3 int) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "PunishService", arg0, arg1, arg2, arg3)
}

// PunishService indicates an expected call of PunishService.
func (mr *MockServiceControllerMockRecorder) PunishService(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PunishService", reflect.TypeOf((*MockServiceController)(nil).PunishService), arg0, arg1, arg2, arg3)
}

// PunishServiceWithThreshold mocks base method.
func (m *MockServiceController) PunishServiceWithThreshold(arg0 context.Context, arg1, arg2 string, arg3 int) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "PunishServiceWithThreshold", arg0, arg1, arg2, arg3)
}

// PunishServiceWithThreshold indicates an expected call of PunishServiceWithThreshold.
func (mr *MockServiceControllerMockRecorder) PunishServiceWithThreshold(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PunishServiceWithThreshold", reflect.TypeOf((*MockServiceController)(nil).PunishServiceWithThreshold), arg0, arg1, arg2, arg3)
}

// MockVolumeGetter is a mock of VolumeGetter interface.
type MockVolumeGetter struct {
	ctrl     *gomock.Controller
	recorder *MockVolumeGetterMockRecorder
}

// MockVolumeGetterMockRecorder is the mock recorder for MockVolumeGetter.
type MockVolumeGetterMockRecorder struct {
	mock *MockVolumeGetter
}

// NewMockVolumeGetter creates a new mock instance.
func NewMockVolumeGetter(ctrl *gomock.Controller) *MockVolumeGetter {
	mock := &MockVolumeGetter{ctrl: ctrl}
	mock.recorder = &MockVolumeGetterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockVolumeGetter) EXPECT() *MockVolumeGetterMockRecorder {
	return m.recorder
}

// Get mocks base method.
func (m *MockVolumeGetter) Get(arg0 context.Context, arg1 proto.Vid, arg2 bool) *controller.VolumePhy {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", arg0, arg1, arg2)
	ret0, _ := ret[0].(*controller.VolumePhy)
	return ret0
}

// Get indicates an expected call of Get.
func (mr *MockVolumeGetterMockRecorder) Get(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockVolumeGetter)(nil).Get), arg0, arg1, arg2)
}

// Punish mocks base method.
func (m *MockVolumeGetter) Punish(arg0 context.Context, arg1 proto.Vid, arg2 int) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Punish", arg0, arg1, arg2)
}

// Punish indicates an expected call of Punish.
func (mr *MockVolumeGetterMockRecorder) Punish(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Punish", reflect.TypeOf((*MockVolumeGetter)(nil).Punish), arg0, arg1, arg2)
}
