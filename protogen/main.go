// Utility to generate the protobuf description of ugc files based on pairs of source and published packs.
//
// This code is mostly a mess.
// I have just one excuse: it was meant to be run only few times and and therefore was not worth much effort.
// Rather irregular structure of ugc files does not make it any better.
package main

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)
	log.Println(os.Args)

	if len(os.Args) < 4 || len(os.Args)%2 != 0 {
		log.Print("Usage:  protogen <output_dir> <source_pack1> <published_pack1> [<source_pack2> <published_pack2>] [...]")
		os.Exit(1)
	}

	types := make(map[string]Type)
	for i := 2; i < len(os.Args)-1; i++ {
		log.Print(i, os.Args[i], os.Args[i+1])
		err := doPack(os.Args[i], os.Args[i+1], types)
		if err != nil {
			log.Fatalf("%v: %v", os.Args[i], err)
		}
	}

	err := ioutil.WriteFile(filepath.Join(os.Args[1], "dump.json"), []byte(dump(types)), 0644)
	if err != nil {
		log.Fatal(err)
	}
}
