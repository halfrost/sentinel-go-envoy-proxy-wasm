package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	// sentinel "sentinel-go-envoy-proxy-wasm/api"
	// "sentinel-go-envoy-proxy-wasm/core/config"
	// "sentinel-go-envoy-proxy-wasm/core/flow"
	// "sentinel-go-envoy-proxy-wasm/logging"
	"time"

	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm/types"
)

func main() {
	proxywasm.SetVMContext(&vmContext{})
}

type vmContext struct {
	types.DefaultVMContext
	contextID uint32
}

func (*vmContext) NewPluginContext(contextID uint32) types.PluginContext {
	return &pluginContext{contextID: contextID}
}

func (*vmContext) OnVMStart(vmConfigurationSize int) types.OnVMStartStatus {
	return types.OnVMStartStatusOK
}

type pluginContext struct {
	types.DefaultPluginContext
	contextID uint32
}

func (*pluginContext) NewTcpContext(contextID uint32) types.TcpContext {
	return &tcpContext{contextID: time.Now().UnixNano()}
}

func (*pluginContext) OnPluginStart(pluginConfigurationSize int) types.OnPluginStartStatus {
	data, err := proxywasm.GetPluginConfiguration()
	if err != nil {
		proxywasm.LogCriticalf("error reading plugin configuration: %v", err)
	}

	proxywasm.LogInfof("plugin config: %s", string(data))
	plugConfig := map[string]string{}
	json.Unmarshal([]byte(data), &plugConfig)
	// proxywasm.LogInfof("plugin configmap: %v", plugConfig["resource_name"])

	// conf := config.NewDefaultConfig()
	// conf.Sentinel.Log.Logger = logging.NewConsoleLogger()
	// err = sentinel.InitWithConfig(conf)
	// if err != nil {
	// 	proxywasm.LogCritical(err.Error())
	// }
	// _, err = flow.LoadRules([]*flow.Rule{
	// 	{
	// 		Resource:               plugConfig["resource_name"],
	// 		TokenCalculateStrategy: flow.Direct,
	// 		ControlBehavior:        flow.Reject,
	// 		Threshold:              1,
	// 		StatIntervalInMs:       1000,
	// 	},
	// })

	// if err != nil {
	// 	proxywasm.LogCritical(err.Error())
	// }
	return types.OnPluginStartStatusOK
}

type tcpContext struct {
	types.DefaultTcpContext
	contextID int64
}

func (ctx *tcpContext) OnNewConnection() types.Action {
	// tcp connection begin
	proxywasm.LogInfo("OnNewConnection")
	initialValueBuf := make([]byte, 8)
	if err := proxywasm.SetSharedData(strconv.FormatInt(ctx.contextID, 10), initialValueBuf, 0); err != nil {
		proxywasm.LogInfof("error setting shared data on OnHttpRequestHeaders: %v", err)
	}

	// data, err := proxywasm.GetPluginConfiguration()
	// if err != nil {
	// 	proxywasm.LogCriticalf("error reading plugin configuration: %v", err)
	// }

	// proxywasm.LogInfof("plugin config: %v", string(data))
	// plugConfig := map[string]string{}
	// json.Unmarshal([]byte(data), &plugConfig)
	// proxywasm.LogInfof("plugin configmap: %v", plugConfig["resource_name"])

	err := setKeyValue(strconv.FormatInt(ctx.contextID, 10), "gooooood")
	proxywasm.LogInfof("setData err=%v", err)

	// go func() {
	// 	for {
	// 		_, b := sentinel.Entry(plugConfig["resource_name"], sentinel.WithTrafficType(base.Inbound))
	// 		if b != nil {
	// 			setData()
	// 		} else {

	// 		}
	// 	}
	// }()
	return types.ActionContinue
}

func (ctx *tcpContext) OnStreamDone() {
	proxywasm.LogInfo("OnStreamDone")
	// tcp connection done, the last one
}

// Override types.DefaultTcpContext.
func (ctx *tcpContext) OnDownstreamData(dataSize int, endOfStream bool) types.Action {
	if dataSize == 0 {
		return types.ActionContinue
	}

	data, err := proxywasm.GetDownstreamData(0, dataSize)
	if err != nil && err != types.ErrorStatusNotFound {
		proxywasm.LogCriticalf("failed to get downstream data: %v", err)
		return types.ActionContinue
	}

	proxywasm.LogInfof(">>>>>> downstream data received >>>>>>\n%s", string(data))
	str, er := getKeyValue(strconv.FormatInt(ctx.contextID, 10))
	proxywasm.LogInfof("------------key = %v str = %v err = %v\n", strconv.FormatInt(ctx.contextID, 10), str, er)
	return types.ActionContinue
}

// Override types.DefaultTcpContext.
func (ctx *tcpContext) OnDownstreamClose(t types.PeerType) {
	proxywasm.LogInfof("downstream connection close! %v\n", t)
	return
}

// Override types.DefaultTcpContext.
func (ctx *tcpContext) OnUpstreamData(dataSize int, endOfStream bool) types.Action {
	if dataSize == 0 {
		return types.ActionContinue
	}

	ret, err := proxywasm.GetProperty([]string{"upstream", "address"})
	if err != nil {
		proxywasm.LogCriticalf("failed to get upstream data: %v", err)
		return types.ActionContinue
	}

	proxywasm.LogInfof("remote address: %s", string(ret))

	data, err := proxywasm.GetUpstreamData(0, dataSize)
	if err != nil && err != types.ErrorStatusNotFound {
		proxywasm.LogCritical(err.Error())
	}

	length := 14
	responseData := fmt.Sprintf("HTTP/1.1 429 Too Many Requests\r\ncontent-length: %v\r\ncontent-type: text/plain\r\ndate: %v\r\nserver: envoy\r\n\r\nXXXXXXXXXXXXX\n", length, time.Now().Format(http.TimeFormat))

	proxywasm.ReplaceUpstreamData([]byte(responseData))
	proxywasm.LogInfof("<<<<<< upstream data received <<<<<<\n%v\n%v", string(responseData), string(data))
	return types.ActionContinue
}

// Override types.DefaultTcpContext.
func (ctx *tcpContext) OnUpstreamClose(t types.PeerType) {
	proxywasm.LogInfof("upstream connection close! %v\n", t)
	return
}

func setKeyValue(key, value string) error {
	_, cas, err := proxywasm.GetSharedData(key)
	if err != nil {
		proxywasm.LogWarnf("error getting shared data on OnHttpRequestHeaders: %v", err)
	}

	if err := proxywasm.SetSharedData(key, []byte(value), cas); err != nil {
		proxywasm.LogWarnf("error setting shared data on OnHttpRequestHeaders: %v", err)
		return err
	}
	return nil
}

func getKeyValue(key string) (string, error) {
	value, _, err := proxywasm.GetSharedData(key)
	if err != nil {
		proxywasm.LogWarnf("error getting shared data on OnHttpRequestHeaders: %v", err)
		return "", err
	}

	return string(value), err
}

type httpHeaders struct {
	types.DefaultHttpContext
	contextID int64
}

func (ctx *httpHeaders) OnHttpRequestHeaders(numHeaders int, endOfStream bool) types.Action {
	// http connection begin
	proxywasm.LogInfof("HTTP OnHttpRequestHeaders numHeaders = %v endOfStream = %v", numHeaders, endOfStream)
	// value, _, err := proxywasm.GetSharedData("shared_data_key")
	// if err != nil {
	// 	proxywasm.LogWarnf("error getting shared data on OnNewConnection: %v", err)
	// }
	// if binary.LittleEndian.Uint64(value) > 0 {
	// 	return types.ActionPause
	// }
	return types.ActionContinue
}

func (ctx *httpHeaders) OnHttpRequestBody(numHeaders int, endOfStream bool) types.Action {
	proxywasm.LogInfof("HTTP OnHttpRequestBody numHeaders = %v endOfStream = %v", numHeaders, endOfStream)
	return types.ActionContinue
}

func (ctx *httpHeaders) OnHttpRequestTrailers(numHeaders int) types.Action {
	proxywasm.LogInfof("HTTP OnHttpRequestTrailers numHeaders = %v", numHeaders)
	return types.ActionContinue
}

func (ctx *httpHeaders) OnHttpResponseHeaders(numHeaders int, endOfStream bool) types.Action {
	proxywasm.LogInfof("OnHttpResponseHeaders numHeaders = %v endOfStream = %v", numHeaders, endOfStream)
	return types.ActionContinue
}

func (ctx *httpHeaders) OnHttpResponseBody(numHeaders int, endOfStream bool) types.Action {
	proxywasm.LogInfof("OnHttpResponseBody numHeaders = %v endOfStream = %v", numHeaders, endOfStream)
	return types.ActionContinue
}

func (ctx *httpHeaders) OnHttpResponseTrailers(numHeaders int) types.Action {
	proxywasm.LogInfof("HTTP OnHttpResponseTrailers numHeaders = %v", numHeaders)
	return types.ActionContinue
}

func (ctx *httpHeaders) OnHttpStreamDone() {
	proxywasm.LogInfof("%v finished", ctx.contextID)
}
