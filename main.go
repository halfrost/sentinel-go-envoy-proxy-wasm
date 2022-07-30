package main

import (
	"encoding/binary"
	"log"
	sentinel "sentinel-go-envoy-proxy-wasm/api"
	"sentinel-go-envoy-proxy-wasm/core/base"
	"sentinel-go-envoy-proxy-wasm/core/config"
	"sentinel-go-envoy-proxy-wasm/core/flow"
	"sentinel-go-envoy-proxy-wasm/logging"

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
	conf := config.NewDefaultConfig()
	conf.Sentinel.Log.Logger = logging.NewConsoleLogger()
	err := sentinel.InitWithConfig(conf)
	if err != nil {
		log.Fatal(err)
	}

	_, err = flow.LoadRules([]*flow.Rule{
		{
			Resource:               "test-flow-qps-resource",
			TokenCalculateStrategy: flow.Direct,
			ControlBehavior:        flow.Reject,
			Threshold:              1,
			StatIntervalInMs:       1000,
		},
	})

	if err != nil {
		log.Fatalf("Unexpected error: %+v", err)
	}
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
func (*pluginContext) NewHttpContext(contextID uint32) types.HttpContext {
	return &httpHeaders{contextID: contextID}
}

// Override types.DefaultPluginContext.
func (*pluginContext) NewTcpContext(contextID uint32) types.TcpContext {
	return &tcpContext{contextID: contextID}
}

func (*pluginContext) OnPluginStart(pluginConfigurationSize int) types.OnPluginStartStatus {
	go func() {
		for {
			_, b := sentinel.Entry("test-flow-qps-resource", sentinel.WithTrafficType(base.Inbound))
			if b != nil {
				incrementData()
			} else {
				resetData()
			}
		}
	}()
	return types.OnPluginStartStatusOK
}

type tcpContext struct {
	types.DefaultTcpContext
	contextID uint32
}

func (ctx *tcpContext) OnNewConnection() types.Action {
	// tcp connection begin
	value, _, err := proxywasm.GetSharedData("shared_data_key")
	if err != nil {
		proxywasm.LogWarnf("error getting shared data on OnNewConnection: %v", err)
	}
	if binary.LittleEndian.Uint64(value) > 0 {
		return types.ActionPause
	}
	return types.ActionContinue
}

func (ctx *tcpContext) OnStreamDone() {
	// tcp connection done
}

type httpContext struct {
	types.DefaultHttpContext
	contextID uint32
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
	contextID uint32
}

func (ctx *httpHeaders) OnHttpRequestHeaders(numHeaders int, endOfStream bool) types.Action {
	proxywasm.LogInfo("OnHttpRequestHeaders")
	return types.ActionContinue
}

func (ctx *httpHeaders) OnHttpResponseHeaders(numHeaders int, endOfStream bool) types.Action {
	proxywasm.LogInfo("OnHttpResponseHeaders")
	return types.ActionContinue
}

func (ctx *httpHeaders) OnHttpStreamDone() {
	proxywasm.LogInfof("%d finished", ctx.contextID)
}
