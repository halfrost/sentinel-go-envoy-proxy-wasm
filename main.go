package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/config"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/alibaba/sentinel-golang/core/hotspot"
	"github.com/alibaba/sentinel-golang/core/isolation"
	"github.com/alibaba/sentinel-golang/core/system"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm/types"
	"gopkg.in/yaml.v2"
)

type Metric int

const (
	RequestAmount Metric = iota
	CpuPercentage
)

type Mode int

const (
	Global Mode = iota
	Local
)

type Strategy int

const (
	SlowRequestRatio Strategy = iota
	ErrorRatio
	ErrorCount
	BBR
)

var (
	strategyMap = map[string]circuitbreaker.Strategy{
		"SlowRequestRatio": circuitbreaker.SlowRequestRatio,
		"ErrorRatio":       circuitbreaker.ErrorRatio,
		"ErrorCount":       circuitbreaker.ErrorCount,
	}
	modeMap = map[string]Mode{
		"Global": Global,
		"Local":  Local,
	}
	metricMap = map[string]hotspot.MetricType{
		"Concurrency": hotspot.Concurrency,
		"QPS":         hotspot.Concurrency,
	}
	tokenCalculateStrategyMap = map[string]flow.TokenCalculateStrategy{
		"Direct":         flow.Direct,
		"WarmUp":         flow.WarmUp,
		"MemoryAdaptive": flow.MemoryAdaptive,
	}
	controlBehaviorMap = map[string]flow.ControlBehavior{
		"Reject":     flow.Reject,
		"Throttling": flow.Throttling,
	}
	relationStrategyMap = map[string]flow.RelationStrategy{
		"CurrentResource":    flow.CurrentResource,
		"AssociatedResource": flow.AssociatedResource,
	}
	adaptiveStrategyMap = map[string]system.AdaptiveStrategy{
		"none": system.NoAdaptive,
		"bbr":  system.BBR,
	}
	systemMetricMap = map[string]system.MetricType{
		"load":        system.Load,
		"avgRT":       system.AvgRT,
		"concurrency": system.Concurrency,
		"inboundQPS":  system.InboundQPS,
		"cpuUsage":    system.CpuUsage,
	}
)

type Kind int

const (
	RateLimitStrategy                  Kind = 0
	ThrottlingStrategy                 Kind = 1
	ConcurrencyLimitStrategy           Kind = 2
	CircuitBreakerStrategy             Kind = 3
	AdaptiveOverloadProtectionStrategy Kind = 4
)

type Pair struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

type Conf struct {
	Apiversion string `yaml:"apiVersion"`
	KindType   string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Behavior     string `yaml:"behavior"`
		BehaviorDesc struct {
			ResponseStatusCode        int    `yaml:"responseStatusCode"`
			ResponseContentBody       string `yaml:"responseContentBody"`
			ResponseAdditionalHeaders []Pair `yaml:"responseAdditionalHeaders"`
		} `yaml:"behaviorDesc"`
		MetricType                   string                `yaml:"metricType"`
		LimitMode                    string                `yaml:"limitMode"`
		Threshold                    float64               `yaml:"threshold"`
		StatDuration                 uint32                `yaml:"statDuration"`          // unit is second
		MinIntervalOfRequests        uint32                `yaml:"minIntervalOfRequests"` // unit is millisecond
		QueueTimeout                 uint32                `yaml:"queueTimeout"`          // unit is millisecond
		RecoveryTimeout              uint32                `yaml:"recoveryTimeout"`       // unit is second
		RetryTimeoutMs               uint32                `yaml:"retryTimeoutMs"`        // unit is millisecond
		StatIntervalMs               uint32                `yaml:"statIntervalMs"`        // unit is millisecond
		MaxConcurrency               uint32                `yaml:"maxConcurrency"`
		StrategyType                 string                `yaml:"strategy"`
		TriggerRatio                 float64               `yaml:"triggerRatio"`
		MinRequestAmount             uint64                `yaml:"minRequestAmount"`
		TriggerThreshold             float64               `yaml:"triggerThreshold"`
		AdaptiveStrategy             string                `yaml:"adaptiveStrategy"`
		StatSlidingWindowBucketCount uint32                `yaml:"statSlidingWindowBucketCount"`
		MaxAllowedRtMs               uint64                `yaml:"maxAllowedRtMs"`
		ProbeNum                     uint64                `yaml:"probeNum"`
		TokenCalculateStrategy       string                `yaml:"tokenCalculateStrategy"`
		ControlBehavior              string                `yaml:"controlBehavior"`
		RelationStrategy             string                `yaml:"relationStrategy"`
		RefResource                  string                `yaml:"refResource"`
		MaxQueueingTimeMs            uint32                `yaml:"maxQueueingTimeMs"`
		WarmUpPeriodSec              uint32                `yaml:"warmUpPeriodSec"`
		WarmUpColdFactor             uint32                `yaml:"warmUpColdFactor"`
		StatIntervalInMs             uint32                `yaml:"statIntervalInMs"`
		LowMemUsageThreshold         int64                 `yaml:"lowMemUsageThreshold"`
		HighMemUsageThreshold        int64                 `yaml:"highMemUsageThreshold"`
		MemLowWaterMarkBytes         int64                 `yaml:"memLowWaterMarkBytes"`
		MemHighWaterMarkBytes        int64                 `yaml:"memHighWaterMarkBytes"`
		ParamIndex                   int                   `yaml:"paramIndex"`
		ParamKey                     string                `yaml:"paramKey"`
		BurstCount                   int64                 `yaml:"burstCount"`
		DurationInSec                int64                 `yaml:"durationInSec"`
		ParamsMaxCapacity            int64                 `yaml:"paramsMaxCapacity"`
		SpecificItems                map[interface{}]int64 `yaml:"specificItems"`
		TriggerCount                 float64               `yaml:"triggerCount"`
		Strategy                     string                `yaml:"strategy"`
		SlowConditions               struct {
			MaxAllowedRt int `yaml:"maxAllowedRt"` // unit is millisecond
		} `yaml:"slowConditions"`
	} `yaml:"spec"`
}

func readYaml(filename string) (*[]Conf, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(file)
	confs, conf := []Conf{}, Conf{}
	err = dec.Decode(&conf)
	for err == nil {
		confs = append(confs, conf)
		fmt.Println(conf)
		conf = Conf{}
		err = dec.Decode(&conf)
	}
	if !errors.Is(err, io.EOF) {
		return nil, err
	}
	fmt.Println("Parse yaml complete!")
	return &confs, nil
}

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

	confs, err := readYaml(plugConfig["config_path"])
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%v", confs)

	flowRules, hotspotRules, circuitbreakerRules, isolationRules, systemRules := []*flow.Rule{}, []*hotspot.Rule{}, []*circuitbreaker.Rule{},
		[]*isolation.Rule{}, []*system.Rule{}
	for _, conf := range *confs {
		switch conf.KindType {
		case "RateLimitStrategy":
			flowRules = append(flowRules, &flow.Rule{
				Resource:               plugConfig["resource_name"],
				TokenCalculateStrategy: tokenCalculateStrategyMap[conf.Spec.TokenCalculateStrategy],
				ControlBehavior:        controlBehaviorMap[conf.Spec.ControlBehavior],
				Threshold:              conf.Spec.Threshold,
				StatIntervalInMs:       conf.Spec.StatIntervalMs,
				RelationStrategy:       relationStrategyMap[conf.Spec.RelationStrategy],
				RefResource:            conf.Spec.RefResource,
				MaxQueueingTimeMs:      conf.Spec.MaxQueueingTimeMs,
				WarmUpPeriodSec:        conf.Spec.WarmUpPeriodSec,
				WarmUpColdFactor:       conf.Spec.WarmUpColdFactor,
				LowMemUsageThreshold:   conf.Spec.LowMemUsageThreshold,
				HighMemUsageThreshold:  conf.Spec.HighMemUsageThreshold,
				MemLowWaterMarkBytes:   conf.Spec.MemLowWaterMarkBytes,
				MemHighWaterMarkBytes:  conf.Spec.MemHighWaterMarkBytes,
			})
		case "ThrottlingStrategy":
			hotspotRules = append(hotspotRules, &hotspot.Rule{
				Resource:          plugConfig["resource_name"],
				MetricType:        hotspot.QPS,
				ControlBehavior:   hotspot.Throttling,
				ParamIndex:        conf.Spec.ParamIndex,
				ParamKey:          conf.Spec.ParamKey,
				Threshold:         int64(conf.Spec.Threshold),
				MaxQueueingTimeMs: int64(conf.Spec.MaxQueueingTimeMs),
				BurstCount:        conf.Spec.BurstCount,
				DurationInSec:     conf.Spec.DurationInSec,
				ParamsMaxCapacity: conf.Spec.ParamsMaxCapacity,
				SpecificItems:     conf.Spec.SpecificItems,
			})
		case "ConcurrencyLimitStrategy":
			isolationRules = append(isolationRules, &isolation.Rule{
				Resource:   plugConfig["resource_name"],
				MetricType: isolation.Concurrency,
				Threshold:  uint32(conf.Spec.Threshold),
			})
		case "CircuitBreakerStrategy":
			circuitbreakerRules = append(circuitbreakerRules, &circuitbreaker.Rule{
				Resource:                     plugConfig["resource_name"],
				Strategy:                     strategyMap[conf.Spec.StrategyType],
				RetryTimeoutMs:               conf.Spec.RetryTimeoutMs,
				MinRequestAmount:             conf.Spec.MinRequestAmount,
				StatIntervalMs:               conf.Spec.StatIntervalMs,
				StatSlidingWindowBucketCount: conf.Spec.StatSlidingWindowBucketCount,
				Threshold:                    conf.Spec.Threshold,
				ProbeNum:                     conf.Spec.ProbeNum,
				MaxAllowedRtMs:               conf.Spec.MaxAllowedRtMs,
			})
		case "AdaptiveOverloadProtectionStrategy":
			systemRules = append(systemRules, &system.Rule{
				MetricType:   systemMetricMap[conf.Spec.MetricType],
				TriggerCount: conf.Spec.TriggerCount,
				Strategy:     adaptiveStrategyMap[conf.Spec.AdaptiveStrategy],
			})
		case "HttpRequestFallbackAction":

		}
	}

	if len(flowRules) > 0 {
		_, err = flow.LoadRules(flowRules)
		if err != nil {
			proxywasm.LogCritical(err.Error())
		}
	}

	if len(hotspotRules) > 0 {
		_, err = hotspot.LoadRules(hotspotRules)
		if err != nil {
			proxywasm.LogCritical(err.Error())
		}
	}

	if len(circuitbreakerRules) > 0 {
		_, err = circuitbreaker.LoadRules(circuitbreakerRules)
		if err != nil {
			proxywasm.LogCritical(err.Error())
		}
	}

	if len(isolationRules) > 0 {
		_, err = isolation.LoadRules(isolationRules)
		if err != nil {
			proxywasm.LogCritical(err.Error())
		}
	}

	if len(systemRules) > 0 {
		_, err = system.LoadRules(systemRules)
		if err != nil {
			proxywasm.LogCritical(err.Error())
		}
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
