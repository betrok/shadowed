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

	case "objects":
		err = PrintObjects()

	case "hex":
		err = PrintHexDump()

	case "grep":
		err = GrepDump()

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

	case "music-pack":
		if len(os.Args) < 5 {
			usage()
		}

		err = MusicPack()

	case "music-parse":
		err = ParseMusicLib()

	case "dump-resources":
		err = DumpResources()

	case "cpack-make-writable":
		err = CPackMakeWritable()

	default:
		log.Print("unknown command")
		usage()
	}

	if err != nil {
		log.Fatal(err)
	}

	log.Print("Done")
}

func usage() {
	log.Print(`Usage:  shadowed <command> [arguments...]

Common commands (should work with most unity assets files with version 9):
    header <assets_file>
        Print file metadata(excluding objects).

    objects <assets_file> [type_id]
        Print objects info.
        Optionaly can filter objects by type_id.

    hex <assets_file> [type_id]
        Print hexdump of objects from the assets file along with meta infomation.
        Optionaly can filter objects by type_id.
        Can take a lot of time on a large file.

    grep <assets_file> <string>
        Print hexdump of objects containing the given string..

    unpack <assets_file> <output_dir>
        Dumps all the objects from the assets file to the output directory.
        Subdirectories and files are named after class/object ids.

Shadowrun-specific commands:
    music-list <data_root>
        List all the music in the resources.assets.

    music-unpack <data_root> <output_dir>
        Unpack all the music tracks from resources.assets{,.resS} to the output directory.

    music-pack <data_root> <music_dir> <output_dir>
        Create a modified version of the resources files from data_root
        by adding new tracks from music_dir and replacing onl ones with same name.
        Place new files to output_dir along with updated music.mlib.bytes.

    dump-resources <data_root>
        Print dump of ResourcesManager from the mainData file.

	cpack-make-writable <project.cpack.bytes>
		Reset read_only flag in project.cpack.bytes.
		Can be used for editing a UGC published by someone else.
`)
	os.Exit(1)
}

func dump(val interface{}) string {
	data, _ := json.MarshalIndent(val, "", "  ")
	return string(data)
}
