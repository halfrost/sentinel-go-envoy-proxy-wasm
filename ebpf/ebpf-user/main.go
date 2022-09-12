package main

import (
	"log"

	"github.com/cilium/ebpf"
)

func main() {
	// outerMap, err := ebpf.NewMap(&outerMapSpec)
	// if err != nil {
	// 	log.Fatalf("outer_map: %v", err)
	// }
	// defer outerMap.Close()

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
