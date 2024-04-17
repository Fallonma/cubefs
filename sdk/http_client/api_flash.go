package http_client

import (
	"encoding/json"
	"fmt"
	"github.com/cubefs/cubefs/proto"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type FlashClient struct {
	sync.RWMutex
	useSSL bool
	host   string
}

// NewFlashClient returns a new FlashClient instance.
func NewFlashClient(host string, useSSL bool) *FlashClient {
	return &FlashClient{host: host, useSSL: useSSL}
}

func (client *FlashClient) RequestHttp(method, path string, param map[string]string) (respData []byte, err error) {
	req := newAPIRequest(method, path)
	for k, v := range param {
		req.addParam(k, v)
	}
	return serveRequest(client.useSSL, client.host, req)
}

func (client *FlashClient) GetStat() (nodeStat *proto.FlashNodeStat, err error) {
	var d []byte
	for i := 0; i < 3; i++ {
		d, err = client.RequestHttp(http.MethodGet, "/stat", nil)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		return
	}
	nodeStat = new(proto.FlashNodeStat)
	if err = json.Unmarshal(d, nodeStat); err != nil {
		return
	}
	return
}

func (client *FlashClient) GetKeys() (keys []interface{}, err error) {
	var d []byte
	for i := 0; i < 3; i++ {
		d, err = client.RequestHttp(http.MethodGet, "/keys", nil)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		return
	}
	keys = make([]interface{}, 0)
	if err = json.Unmarshal(d, &keys); err != nil {
		return
	}
	return
}

func (client *FlashClient) GetVersion() (version *proto.VersionValue, err error) {
	var data []byte
	data, err = doGet(fmt.Sprintf("http://%v/version", client.host))
	if err != nil {
		return
	}
	version = &proto.VersionValue{}
	err = json.Unmarshal(data, version)
	return
}

func (client *FlashClient) EvictVol(volume string) (err error) {
	params := make(map[string]string)
	params["volume"] = volume
	_, err = client.RequestHttp(http.MethodGet, "/evictVol", params)
	if err != nil {
		return
	}
	return
}

func (client *FlashClient) EvictInode(volume string, inode uint64) (err error) {
	params := make(map[string]string)
	params["volume"] = volume
	params["inode"] = strconv.FormatUint(inode, 10)
	_, err = client.RequestHttp(http.MethodGet, "/evictInode", params)
	if err != nil {
		return
	}
	return
}

func (client *FlashClient) EvictAll() (err error) {
	_, err = client.RequestHttp(http.MethodGet, "/evictAll", nil)
	if err != nil {
		return
	}
	return
}

func (client *FlashClient) SetFlashNodePing(enable bool) (err error) {
	resp, err := doGet(fmt.Sprintf("http://%v/ping/set?enable=%v", client.host, enable))
	if err != nil {
		return
	}
	if !strings.Contains(string(resp), "success") {
		err = fmt.Errorf("SetFlashNodePing failed: %v", string(resp))
	}
	return
}

func (client *FlashClient) SetFlashNodeReadTimeout(ms int) (err error) {
	resp, err := doGet(fmt.Sprintf("http://%v/singleContext/setTimeout?ms=%v", client.host, ms))
	if err != nil {
		return
	}
	if !strings.Contains(string(resp), "success") {
		err = fmt.Errorf("SetFlashNodeReadTimeout failed: %v", string(resp))
	}
	return
}

func (client *FlashClient) SetFlashNodeStack(enable bool) (err error) {
	params := make(map[string]string)
	params["enable"] = strconv.FormatBool(enable)
	resp, err := client.RequestHttp(http.MethodGet, "/stack/set", params)
	if err != nil {
		return
	}
	if !strings.Contains(string(resp), "success") {
		err = fmt.Errorf("SetFlashNodeStack failed: %v", string(resp))
	}
	return
}

func doGet(url string) (data []byte, err error) {
	var resp *http.Response
	if resp, err = http.Get(url); err != nil {
		return
	}
	defer resp.Body.Close()
	data, err = ioutil.ReadAll(resp.Body)
	return
}
