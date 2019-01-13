package main

import (
	"encoding/binary"
	"encoding/hex"
	"github.com/pkg/errors"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
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

func ParseMusicDescription(r io.Reader, order binary.ByteOrder) (desc MusicDescription, err error) {
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

func MusicList() error {
	// In theory some of objects can be in separate files, but it does not seem to be a thing for the shadowrun music.
	assets, err := NewAssetsReader(path.Join(os.Args[2], "resources.assets"))
	if err != nil {
		return err
	}
	defer assets.Close()

	return assets.RangeObjects(func(desc Object, r io.Reader) error {
		if desc.TypeID == MusicTypeID {
			m, err := ParseMusicDescription(r, assets.Order)
			if err != nil {
				return err
			}
			log.Printf("%+v", m)
		}

		return nil
	})
}

func MusicUnpack() error {
	assets, err := NewAssetsReader(path.Join(os.Args[2], "resources.assets"))
	if err != nil {
		return err
	}
	defer assets.Close()

	err = os.MkdirAll(os.Args[3], 0777)
	if err != nil {
		return err
	}

	pack, err := os.Open(path.Join(os.Args[2], "resources.assets.resS"))
	if err != nil {
		return err
	}
	defer pack.Close()

	return assets.RangeObjects(func(desc Object, r io.Reader) error {
		if desc.TypeID == MusicTypeID {
			m, err := ParseMusicDescription(r, assets.Order)
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
}
