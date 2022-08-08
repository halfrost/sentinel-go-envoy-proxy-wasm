package main

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"time"

	// sentinel "sentinel-go-envoy-proxy-wasm/api"
	// "sentinel-go-envoy-proxy-wasm/core/base"
	// "sentinel-go-envoy-proxy-wasm/core/config"
	// "sentinel-go-envoy-proxy-wasm/core/flow"
	// "sentinel-go-envoy-proxy-wasm/logging"

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

// Override types.DefaultVMContext.
func (*vmContext) NewPluginContext(contextID uint32) types.PluginContext {
	return &pluginContext{contextID: contextID}
}

// Override types.VMContext.
func (*vmContext) OnVMStart(vmConfigurationSize int) types.OnVMStartStatus {
	// conf := config.NewDefaultConfig()
	// conf.Sentinel.Log.Logger = logging.NewConsoleLogger()
	// err := sentinel.InitWithConfig(conf)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// _, err = flow.LoadRules([]*flow.Rule{
	// 	{
	// 		Resource:               "test-flow-qps-resource",
	// 		TokenCalculateStrategy: flow.Direct,
	// 		ControlBehavior:        flow.Reject,
	// 		Threshold:              1,
	// 		StatIntervalInMs:       1000,
	// 	},
	// })

	// if err != nil {
	// 	log.Fatalf("Unexpected error: %+v", err)
	// }

	// initialValueBuf := make([]byte, 8)
	// binary.LittleEndian.PutUint64(initialValueBuf, sharedDataInitialValue)
	// if err := proxywasm.SetSharedData(sharedDataKey, initialValueBuf, 0); err != nil {
	// 	proxywasm.LogWarnf("error setting shared data on OnVMStart: %v", err)
	// }
	return types.OnVMStartStatusOK
}

type pluginContext struct {
	types.DefaultPluginContext
	contextID uint32
}

// Override types.DefaultPluginContext.
// func (*pluginContext) NewHttpContext(contextID uint32) types.HttpContext {
// 	return &httpHeaders{contextID: time.Now().UnixNano()}
// }

// Override types.DefaultPluginContext.
func (*pluginContext) NewTcpContext(contextID uint32) types.TcpContext {
	return &tcpContext{contextID: time.Now().UnixNano()}
}

func (*pluginContext) OnPluginStart(pluginConfigurationSize int) types.OnPluginStartStatus {
	// go func() {
	// 	for {
	// 		_, b := sentinel.Entry("test-flow-qps-resource", sentinel.WithTrafficType(base.Inbound))
	// 		if b != nil {
	// 			incrementData()
	// 		} else {
	// 			resetData()
	// 		}
	// 	}
	// }()
	return types.OnPluginStartStatusOK
}

type tcpContext struct {
	types.DefaultTcpContext
	contextID int64
}

func (ctx *tcpContext) OnNewConnection() types.Action {
	// tcp connection begin
	proxywasm.LogInfo("OnNewConnection")
	// value, _, err := proxywasm.GetSharedData("shared_data_key")
	// if err != nil {
	// 	proxywasm.LogWarnf("error getting shared data on OnNewConnection: %v", err)
	// }
	// if binary.LittleEndian.Uint64(value) > 0 {
	// 	return types.ActionPause
	// }
	return types.ActionContinue
}

func (ctx *tcpContext) OnStreamDone() {
	proxywasm.LogInfo("OnStreamDone")
	// tcp connection done
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
	// proxywasm.CloseDownstream()
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

	length := 13
	responseData := fmt.Sprintf("HTTP/1.1 429 Too Many Requests\r\ncontent-length: %v\r\ncontent-type: text/plain\r\ndate: %v\r\nserver: envoy\r\n\r\nXXXXXXXXXXXXX", length, time.Now().Format(http.TimeFormat))

	proxywasm.ReplaceUpstreamData([]byte(responseData))
	proxywasm.LogInfof("<<<<<< upstream data received <<<<<<\n%v\n%v", string(responseData), string(data))
	return types.ActionContinue
}

// Override types.DefaultTcpContext.
func (ctx *tcpContext) OnUpstreamClose(t types.PeerType) {
	proxywasm.LogInfof("upstream connection close! %v\n", t)
	return
}

func incrementData() (uint64, error) {
	value, cas, err := proxywasm.GetSharedData("shared_data_key")
	if err != nil {
		proxywasm.LogWarnf("error getting shared data on OnHttpRequestHeaders: %v", err)
		return 0, err
	}

	buf := make([]byte, 8)
	ret := binary.LittleEndian.Uint64(value) + 1
	binary.LittleEndian.PutUint64(buf, ret)
	if err := proxywasm.SetSharedData("threshold", buf, cas); err != nil {
		proxywasm.LogWarnf("error setting shared data on OnHttpRequestHeaders: %v", err)
		return 0, err
	}
	return ret, err
}

func resetData() (uint64, error) {
	_, cas, err := proxywasm.GetSharedData("threshold")
	if err != nil {
		proxywasm.LogWarnf("error getting shared data on OnHttpRequestHeaders: %v", err)
		return 0, err
	}

	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, 0)
	if err := proxywasm.SetSharedData("threshold", buf, cas); err != nil {
		proxywasm.LogWarnf("error setting shared data on OnHttpRequestHeaders: %v", err)
		return 0, err
	}
	return 0, err
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
