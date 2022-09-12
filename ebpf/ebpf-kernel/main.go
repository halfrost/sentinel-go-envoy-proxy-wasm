//go:build linux
// +build linux

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
	
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

// $BPF_CLANG and $BPF_CFLAGS are set by the Makefile.
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc $BPF_CLANG -cflags $BPF_CFLAGS -type event bpf fentry.c -- -I../headers

func main() {
	stopper := make(chan os.Signal, 1)
	signal.Notify(stopper, os.Interrupt, syscall.SIGTERM)

	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatal(err)
	}

	objs := bpfObjects{}
	if err := loadBpfObjects(&objs, nil); err != nil {
		log.Fatalf("loading objects: %v", err)
	}
	defer objs.Close()

	link, err := link.AttachTracing(link.TracingOptions{
		Program: objs.bpfPrograms.TcpConnect,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer link.Close()

	rd, err := ringbuf.NewReader(objs.bpfMaps.Events)
	if err != nil {
		log.Fatalf("opening ringbuf reader: %s", err)
	}
	defer rd.Close()

	go func() {
		<-stopper

		if err := rd.Close(); err != nil {
			log.Fatalf("closing ringbuf reader: %s", err)
		}
	}()

	log.Printf("%-16s %-15s %-6s -> %-15s %-6s",
		"Comm",
		"Src addr",
		"Port",
		"Dest addr",
		"Port",
	)

	var event bpfEvent
	for {
		record, err := rd.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				log.Println("received signal, exiting..")
				return
			}
			log.Printf("reading from reader: %s", err)
			continue
		}

		if err := binary.Read(bytes.NewBuffer(record.RawSample), binary.BigEndian, &event); err != nil {
			log.Printf("parsing ringbuf event: %s", err)
			continue
		}

		log.Printf("%-16s %-15s %-6d -> %-15s %-6d",
			event.Comm,
			intToIP(event.Saddr),
			event.Sport,
			intToIP(event.Daddr),
			event.Dport,
		)
		saveDataToMap(event.Dport, intToIP(event.Saddr), event.Sport)
	}
}

func intToIP(ipNum uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, ipNum)
	return ip
}

func saveDataToMap(desPort, srcAddr, port string) {
		// Allow the current process to lock memory for eBPF resources.
		if err := rlimit.RemoveMemlock(); err != nil {
			log.Fatal(err)
		}
	
		outerMapSpec := ebpf.MapSpec{
			Name:       "outer_map",
			Type:       ebpf.ArrayOfMaps,
			KeySize:    10000,
			ValueSize:  10000,
			MaxEntries: 10000,
			Contents:   make([]ebpf.MapKV, 5),
			InnerMap: &ebpf.MapSpec{
				Name:      "inner_map",
				Type:      ebpf.Array,
				KeySize:   100,
				ValueSize: 100,
				Flags: unix.BPF_F_INNER_MAP,
				MaxEntries: 1,
			},
		}
	
		innerMapSpec := outerMapSpec.InnerMap.Copy()
		innerMapSpec.MaxEntries = 1000
		innerMapSpec.Contents = make([]ebpf.MapKV, innerMapSpec.MaxEntries)
		innerMapSpec = append(innerMapSpec, ebpf.MapKV{Key: desPort, Value: srcAddr+port})
		innerMap, err := ebpf.NewMap(innerMapSpec)
		if err != nil {
			log.Fatalf("inner_map: %v", err)
		}
		defer innerMap.Close()
		outerMapSpec.Contents = append(outerMapSpec.Contents, ebpf.MapKV{Key: i, Value: innerMap})
}

func readMap(outerMapSpec ebpf.MapSpec) {
	outerMap, err := ebpf.NewMap(&outerMapSpec)
	if err != nil {
		log.Fatalf("outer_map: %v", err)
	}
	defer outerMap.Close()

	mapIter := outerMap.Iterate()
	var outerMapKey uint32
	var innerMapID ebpf.MapID
	for mapIter.Next(&outerMapKey, &innerMapID) {
		innerMap, err := ebpf.NewMapFromID(innerMapID)
		if err != nil {
			log.Fatal(err)
		}
		innerMapInfo, err := innerMap.Info()
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("outerMapKey %d, innerMap.Info: %+v", outerMapKey, innerMapInfo.)
	}
}