package main

import (
	"log"
	"math/rand"
	"time"

	"golang.org/x/sys/unix"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
)

func main() {
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatal(err)
	}
	outerMapSpec := ebpf.MapSpec{
		Name:       "outer_map",
		Type:       ebpf.ArrayOfMaps,
		KeySize:    4,
		ValueSize:  4,
		MaxEntries: 5,
		Contents:   make([]ebpf.MapKV, 5),
		InnerMap: &ebpf.MapSpec{
			Name:       "inner_map",
			Type:       ebpf.Array,
			KeySize:    4,
			ValueSize:  4,
			Flags:      unix.BPF_F_INNER_MAP,
			MaxEntries: 1,
		},
	}

	rand.Seed(time.Now().UnixNano())

	for i := uint32(0); i < outerMapSpec.MaxEntries; i++ {
		innerMapSpec := outerMapSpec.InnerMap.Copy()
		innerMapSpec.MaxEntries = uint32(rand.Intn(50) + 1)
		innerMapSpec.Contents = make([]ebpf.MapKV, innerMapSpec.MaxEntries)
		for j := range innerMapSpec.Contents {
			innerMapSpec.Contents[uint32(j)] = ebpf.MapKV{Key: uint32(j), Value: uint32(0xCAFE)}
		}

		innerMap, err := ebpf.NewMap(innerMapSpec)
		if err != nil {
			log.Fatalf("inner_map: %v", err)
		}
		defer innerMap.Close()

		outerMapSpec.Contents[i] = ebpf.MapKV{Key: i, Value: innerMap}
	}

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

		log.Printf("outerMapKey %d, innerMap.Info: %+v", outerMapKey, innerMapInfo)
	}
}
