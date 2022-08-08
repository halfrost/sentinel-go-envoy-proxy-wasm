package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	sentinel "sentinel-go-envoy-proxy-wasm/api"
	"sentinel-go-envoy-proxy-wasm/core/base"
	"sentinel-go-envoy-proxy-wasm/core/config"
	"sentinel-go-envoy-proxy-wasm/core/flow"
	"sentinel-go-envoy-proxy-wasm/logging"
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
	proxywasm.LogInfof("plugin configmap: %v", plugConfig["resource_name"])

	conf := config.NewDefaultConfig()
	conf.Sentinel.Log.Logger = logging.NewConsoleLogger()
	err = sentinel.InitWithConfig(conf)
	if err != nil {
		proxywasm.LogCritical(err.Error())
	}
	_, err = flow.LoadRules([]*flow.Rule{
		{
			Resource:               plugConfig["resource_name"],
			TokenCalculateStrategy: flow.Direct,
			ControlBehavior:        flow.Reject,
			Threshold:              1,
			StatIntervalInMs:       1000,
		},
	})

	if err != nil {
		proxywasm.LogCritical(err.Error())
	}
	return types.OnPluginStartStatusOK
}

type tcpContext struct {
	types.DefaultTcpContext
	contextID  int64
	entry      *base.SentinelEntry
	blockError *base.BlockError
}

func (ctx *tcpContext) OnNewConnection() types.Action {
	proxywasm.LogInfo("OnNewConnection")
	initialValueBuf := make([]byte, 1)
	if err := proxywasm.SetSharedData(strconv.FormatInt(ctx.contextID, 10), initialValueBuf, 0); err != nil {
		proxywasm.LogInfof("error setting shared data on OnNewConnection: %v", err)
	}

	data, err := proxywasm.GetPluginConfiguration()
	if err != nil {
		proxywasm.LogCriticalf("error reading plugin configuration: %v", err)
	}

	proxywasm.LogInfof("plugin config: %v", string(data))
	plugConfig := map[string]string{}
	json.Unmarshal([]byte(data), &plugConfig)
	proxywasm.LogInfof("plugin configmap: %v", plugConfig["resource_name"])

	go func() {
		for {
			e, b := sentinel.Entry(plugConfig["resource_name"], sentinel.WithTrafficType(base.Inbound))
			ctx.entry = e
			ctx.blockError = b
			if b != nil {
				err := setBlockError(strconv.FormatInt(ctx.contextID, 10), ctx.blockError.Error())
				if err != nil {
					proxywasm.LogInfof("set blockError err=%v", err)
				}
			}
		}
	}()
	return types.ActionContinue
}

func (ctx *tcpContext) OnStreamDone() {
	str, err := getBlockError(strconv.FormatInt(ctx.contextID, 10))
	if err != nil {
		proxywasm.LogInfof("get BlockError err = %v\n", err)
	}

	if str == "" {
		ctx.entry.Exit()
	}
	proxywasm.LogInfo("OnStreamDone")
}

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

	proxywasm.LogInfof("<<<<<< upstream data received <<<<<<\n%v", string(data))

	blockErr, err := getBlockError(strconv.FormatInt(ctx.contextID, 10))
	if err != nil {
		proxywasm.LogInfof("get BlockError err = %v\n", err)
	}

	proxywasm.LogInfof("get BlockError = %v\n", blockErr)

	if blockErr != "" {
		responseData := fmt.Sprintf("HTTP/1.1 429 Too Many Requests\r\ncontent-length: %v\r\ncontent-type: text/plain\r\ndate: %v\r\nserver: envoy\r\n\r\n%v\n", len(blockErr)+1, time.Now().Format(http.TimeFormat), blockErr)
		proxywasm.ReplaceUpstreamData([]byte(responseData))
	}
	return types.ActionContinue
}

func setBlockError(key, value string) error {
	_, cas, err := proxywasm.GetSharedData(key)
	if err != nil {
		proxywasm.LogWarnf("error getting shared data on OnNewConnection: %v", err)
	}

	if err := proxywasm.SetSharedData(key, []byte(value), cas); err != nil {
		proxywasm.LogWarnf("error setting shared data on OnNewConnection: %v", err)
		return err
	}
	return nil
}

func getBlockError(key string) (string, error) {
	value, _, err := proxywasm.GetSharedData(key)
	if err != nil {
		proxywasm.LogWarnf("error getting shared data: %v", err)
		return "", err
	}
	return string(value), err
}
