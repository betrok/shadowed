package main

import (
	"encoding/json"
	"log"
	"os"
)

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)
	log.Println(os.Args)

	if len(os.Args) < 3 {
		usage()
	}

	var err error

	switch os.Args[1] {
	case "header":
		err = PrintHeader()

	case "meta":
		err = PrintMeta()

	case "hex":
		err = PrintHexDump()

	case "unpack":
		if len(os.Args) < 4 {
			usage()
		}

		err = UnpackAssets()

	case "music-list":
		err = MusicList()

	case "music-unpack":
		if len(os.Args) < 4 {
			usage()
		}

		err = MusicUnpack()

	default:
		log.Print("unknown command")
		usage()
	}

	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	log.Print(`Usage:  shadowed <command> [argumants...]

Common commands (should work with most unity assets files within versions 9-13):
    header <assets_file>
        Print file header.

    meta <assets_file> [type_id]
        Print all the meta information from the assets file.
        Optionaly can filter objects by type_id.

    hex <assets_file> [type_id]
        Print hexdump of objects from the assets file along with meta infomation.
        Optionaly can filter objects by type_id.
        Can take a lot of memory and time on a large file.

    unpack <assets_file> <output_dir>
        Dumps all the objects from the assets file to the output directory.
        Subdirectories and files are named after class/object ids.

Shadowrun-specific commands:
    music-list <data_root>
        List all the music in the main content pack.

    music-unpack <data_root> <output_dir>
        Unpack all the music tracks from the main content pack to the output directory.
`)
	os.Exit(1)
}

func dump(val interface{}) string {
	data, _ := json.MarshalIndent(val, "", "  ")
	return string(data)
}
