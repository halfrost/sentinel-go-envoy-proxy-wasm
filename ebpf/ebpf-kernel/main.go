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
	if len(os.Args) < 2 {
		log.Fatalf("Please specify a network interface")
	}

	ifaceName := os.Args[1]
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		log.Fatalf("lookup network iface %q: %s", ifaceName, err)
	}

	objs := bpfObjects{}
	if err := loadBpfObjects(&objs, nil); err != nil {
		log.Fatalf("loading objects: %s", err)
	}
	defer objs.Close()

	l, err := link.AttachXDP(link.XDPOptions{
		Program:   objs.XdpProgFunc,
		Interface: iface.Index,
	})
	if err != nil {
		log.Fatalf("could not attach XDP program: %s", err)
	}
	defer l.Close()

	log.Printf("Attached XDP program to iface %q (index %d)", iface.Name, iface.Index)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s, err := formatMapContents(objs.XdpStatsMap)
		if err != nil {
			log.Printf("Error reading map: %s", err)
			continue
		}
		log.Printf("Map contents:\n%s", s)
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

func formatMapContents(m *ebpf.Map) (string, error) {
	var (
		sb  strings.Builder
		key []byte
		val uint32
	)
	iter := m.Iterate()
	for iter.Next(&key, &val) {
		sourceIP := net.IP(key) // IPv4 source address in network byte order.
		packetCount := val
		sb.WriteString(fmt.Sprintf("\t%s => %d\n", sourceIP, packetCount))
	}
	return sb.String(), iter.Err()
}