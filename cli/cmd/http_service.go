package cmd

import (
	"context"
	"fmt"

	"github.com/cubefs/cubefs/proto"
	"github.com/cubefs/cubefs/sdk/master"
	"github.com/cubefs/cubefs/util"
)

type clientHandler interface {
	excuteHttp(_ context.Context) (err error)
}

type volumeClient struct {
	name        string
	capacity    uint64
	opCode      MasterOp
	client      *master.MasterClient
	clientIDKey string
}

func NewVolumeClient(opCode MasterOp, client *master.MasterClient) (vol *volumeClient) {
	vol = new(volumeClient)
	vol.opCode = opCode
	vol.client = client
	return
}

func (vol *volumeClient) excuteHttp(ctx context.Context) (err error) {
	switch vol.opCode {
	case OpExpandVol:
		var vv *proto.SimpleVolView
		if vv, err = vol.client.AdminAPI().GetVolumeSimpleInfo(ctx, vol.name); err != nil {
			return
		}
		if vol.capacity <= vv.Capacity {
			return fmt.Errorf("Expand capacity must larger than %v", vv.Capacity)
		}
		if err = vol.client.AdminAPI().VolExpand(ctx, vol.name, vol.capacity, util.CalcAuthKey(vv.Owner), vol.clientIDKey); err != nil {
			return
		}
	case OpShrinkVol:
		var vv *proto.SimpleVolView
		if vv, err = vol.client.AdminAPI().GetVolumeSimpleInfo(ctx, vol.name); err != nil {
			return
		}
		if vol.capacity >= vv.Capacity {
			return fmt.Errorf("Expand capacity must less than %v", vv.Capacity)
		}
		if err = vol.client.AdminAPI().VolShrink(ctx, vol.name, vol.capacity, util.CalcAuthKey(vv.Owner), vol.clientIDKey); err != nil {
			return
		}
	case OpDeleteVol:
	default:
	}
	return
}
