package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"github.com/pkg/errors"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
)

const MusicTypeID = 83

// Unknown fields data. It's same in all versions that I have.
var MagicNumbers = [4]uint32{2, 14, 0, 2}

type MusicDescription struct {
	Name string

	//These fields probably describe the file format, but I have no clue what exactly they mean.
	// See MagicNumbers above.
	Unknown [4]uint32

	Size  uint32
	Shift uint32
}

func ParseMusicDesc(r io.Reader, order binary.ByteOrder) (desc MusicDescription, err error) {
	var strLen uint32
	err = binary.Read(r, order, &strLen)
	if err != nil {
		return
	}
	// include padding
	buf := make([]byte, (strLen+3)/4*4)
	_, err = io.ReadFull(r, buf)
	if err != nil {
		return
	}
	desc.Name = string(buf[:strLen])

	for i := range desc.Unknown {
		err = binary.Read(r, order, &desc.Unknown[i])
		if err != nil {
			return
		}
	}

	err = binary.Read(r, order, &desc.Size)
	if err != nil {
		return
	}
	err = binary.Read(r, order, &desc.Shift)
	if err != nil {
		return
	}

	// Any extra data will indicate an error
	data, err := ioutil.ReadAll(r)
	if len(data) > 0 {
		err = errors.Errorf("unexpected data left:\n%v", hex.Dump(data))
	}

	return
}

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)
	log.Println(os.Args)

	if len(os.Args) < 3 {
		usage()
	}

	switch os.Args[1] {
	case "header":
		assets, err := NewAssetsReader(os.Args[2])
		if err != nil {
			log.Fatal(err)
		}

		defer assets.Close()
		log.Print(dump(assets.Header))

	case "meta":
		assets, err := NewAssetsReader(os.Args[2])
		if err != nil {
			log.Fatal(err)
		}
		defer assets.Close()

		var filterByType uint64
		if len(os.Args) > 3 {
			filterByType, err = strconv.ParseUint(os.Args[3], 10, 32)
			if err != nil {
				log.Fatalf("invalid type_id: %v", filterByType)
			}
		}

		err = assets.RangeObjects(func(desc Object, r io.Reader) error {
			if filterByType != 0 && desc.TypeID != uint32(filterByType) {
				return nil
			}
			log.Printf("%+v", desc)
			return nil
		})

		if err != nil {
			log.Fatal(err)
		}

	case "hex":
		assets, err := NewAssetsReader(os.Args[2])
		if err != nil {
			log.Fatal(err)
		}
		defer assets.Close()

		var filterByType uint64
		if len(os.Args) > 3 {
			filterByType, err = strconv.ParseUint(os.Args[3], 10, 32)
			if err != nil {
				log.Fatalf("invalid type_id: %v", filterByType)
			}
		}

		err = assets.RangeObjects(func(desc Object, r io.Reader) error {
			if filterByType != 0 && desc.TypeID != uint32(filterByType) {
				return nil
			}
			data, err := ioutil.ReadAll(r)
			if err != nil {
				return err
			}
			log.Printf("%+v\n%v", desc, hex.Dump(data))
			return nil
		})

		if err != nil {
			log.Fatal(err)
		}

	case "unpack":
		if len(os.Args) < 4 {
			usage()
		}

		assets, err := NewAssetsReader(os.Args[2])
		if err != nil {
			log.Fatal(err)
		}
		defer assets.Close()

		err = os.MkdirAll(os.Args[3], 0777)
		if err != nil {
			log.Fatal(err)
		}

		err = assets.RangeObjects(func(desc Object, r io.Reader) error {
			dir := path.Join(os.Args[3], strconv.FormatUint(uint64(desc.TypeID), 10))
			err := os.MkdirAll(dir, 0777)
			if err != nil {
				return err
			}

			file := path.Join(dir, strconv.FormatUint(uint64(desc.ID), 10))
			out, err := os.Create(file)
			if err != nil {
				return err
			}

			_, err = io.Copy(out, r)
			if err != nil {
				out.Close()
				return err
			}

			return out.Close()
		})

		if err != nil {
			log.Fatal(err)
		}

	case "music-list":
		assets, err := NewAssetsReader(path.Join(os.Args[2], "resources.assets"))
		if err != nil {
			log.Fatal(err)
		}
		defer assets.Close()

		err = assets.RangeObjects(func(desc Object, r io.Reader) error {
			if desc.TypeID == MusicTypeID {
				m, err := ParseMusicDesc(r, assets.Order)
				if err != nil {
					return err
				}
				log.Printf("%+v", m)
			}

			return nil
		})

		if err != nil {
			log.Fatal(err)
		}

	case "music-unpack":
		if len(os.Args) < 4 {
			usage()
		}

		assets, err := NewAssetsReader(path.Join(os.Args[2], "resources.assets"))
		if err != nil {
			log.Fatal(err)
		}
		defer assets.Close()

		err = os.MkdirAll(os.Args[3], 0777)
		if err != nil {
			log.Fatal(err)
		}

		pack, err := os.Open(path.Join(os.Args[2], "resources.assets.resS"))
		if err != nil {
			log.Fatal(err)
		}
		defer pack.Close()

		err = assets.RangeObjects(func(desc Object, r io.Reader) error {
			if desc.TypeID == MusicTypeID {
				m, err := ParseMusicDesc(r, assets.Order)
				if err != nil {
					return err
				}

				log.Printf("%+v", m)

				_, err = pack.Seek(int64(m.Shift), 0)
				if err != nil {
					return err
				}

				file := path.Join(os.Args[3], m.Name+".ogg")
				out, err := os.Create(file)
				if err != nil {
					return err
				}

				_, err = io.CopyN(out, pack, int64(m.Size))
				if err != nil {
					out.Close()
					return err
				}

				return out.Close()
			}

			return nil
		})

		if err != nil {
			log.Fatal(err)
		}

	default:
		log.Print("unknown command")
		usage()
	}
}

func usage() {
	log.Print(`Usage:  shadowed <command> [argumants...]

Common commands (should work with most unity assets files within versions 9-13):
    header <assets_file>
        Prints file header.

    meta <assets_file> [type_id]
        Prints all the meta information a the assets file.
        Optionaly can filter objects by type_id.

    hex <assets_file> [type_id]
        Prints hexdump of objects from a assets file along with meta infomation.
        Optionaly can filter objects by type_id.
        Can take a lot of memory and time on a large file.

    unpack <assets_file> <output_dir>
        Dumps all the objects from a assets file to the output directory.
        Subdirectories and files are named after class/object ids.

Shadowrun-specific commands:
    music-list <data_root>
        List all the music in the main content pack.

    music-unpack <data_root> <output_dir>
        Unpack all the music tracks from the main content pack to the output direcoty.
`)
	os.Exit(1)
}

func dump(val interface{}) string {
	data, _ := json.MarshalIndent(val, "", "  ")
	return string(data)
}
