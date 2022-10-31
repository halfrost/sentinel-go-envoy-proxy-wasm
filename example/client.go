package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	qps      int
	path     string
	port     int
	totalNum int64
)

func init() {
	flag.IntVar(&qps, "q", 0, "qps settings")
	flag.StringVar(&path, "u", "", "url path")
	flag.IntVar(&port, "p", 8080, "port")
	flag.Parse()
}

func listenSysSignals(cancel context.CancelFunc) {
	signalChan := make(chan os.Signal, 1)
	ignoreChan := make(chan os.Signal, 1)

	signal.Notify(ignoreChan, syscall.SIGHUP)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	select {
	case sig := <-signalChan:
		fmt.Printf("main exit due to system signal occur: %s\n", sig)
		cancel()
	case sig := <-ignoreChan:
		fmt.Printf("ignore system signal: %s\n", sig)
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < 200*qps; i++ {
		go startQPSTest(ctx)
	}
	go startStatistics(ctx)
	listenSysSignals(cancel)
}

func startStatistics(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 1)
	defer ticker.Stop()

	var counter int64
	for {
		select {
		case <-ticker.C:
			fmt.Printf("QPS: %v | totalNum: %v\n", totalNum-counter, totalNum)
			counter = totalNum
		case <-ctx.Done():
			fmt.Printf("Child startStatistics goroutine exit.\n")
			return
		}
	}
}

func startQPSTest(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 1)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			resp, err := http.Get("http://localhost:" + strconv.Itoa(port) + "/" + path)
			if err != nil {
				fmt.Println(err)
			}
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				fmt.Println(err)
			}
			fmt.Printf("body = %v | statusCode = %v\n", string(body), resp.StatusCode)
			atomic.AddInt64(&totalNum, 1)
		case <-ctx.Done():
			fmt.Printf("Child startQPSTest goroutine exit.\n")
			return
		}
	}
}
