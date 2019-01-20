package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"github.com/betrok/shadowed/class"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"sort"
	"strings"
)

const (
	MainData       = "mainData"
	AssetsFile     = "resources.assets"
	AssetsDataFile = AssetsFile + ".resS"

	MusicLibPath = "StreamingAssets/ContentPacks/shadowrun_core/data/misc"
	MusicLibName = "music.mlib.bytes"
)

const (
	// AudioClip type
	MusicTypeID           = 83
	ResourceManagerTypeID = 147
)

// Unknown fields data. It's same in all versions that I have.
var MagicNumbers = [4]uint32{2, 14, 0, 2}

type MusicDescription struct {
	Name string

	// These fields describe the file format and loading options,
	// but I have no clue what exactly they mean.
	// Gist below has related info("classID{83}").
	// https://gist.githubusercontent.com/Mischanix/7db0145e809b692b63f2/raw/0ae1905171cc38dbfb68994c3cb679c3b8bf9e0c/structs.dump
	// See MagicNumbers above.
	Unknown [4]uint32

	Size  uint32
	Shift uint32
}

func ParseMusicDescription(r io.ReadSeeker, order binary.ByteOrder) (desc MusicDescription, err error) {
	err = read(r, &desc, order, false)
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

func (m MusicDescription) Bytes(order binary.ByteOrder) []byte {
	var buf bytes.Buffer

	write(&buf, m, order, false)

	return buf.Bytes()
}

func MusicList() error {
	// In theory some of objects can be in separate files, but it does not seem to be a thing for the shadowrun music.
	assets, err := NewAssetsReader(path.Join(os.Args[2], AssetsFile))
	if err != nil {
		return err
	}
	defer assets.Close()

	return assets.RangeObjects(func(desc Object, r io.ReadSeeker) error {
		if desc.TypeID == MusicTypeID {
			m, err := ParseMusicDescription(r, assets.Order)
			if err != nil {
				return err
			}
			log.Printf("%+v", desc)
			log.Printf("%+v: %+v", desc.ID, m)
		}

		return nil
	})
}

func MusicUnpack() error {
	assets, err := NewAssetsReader(path.Join(os.Args[2], AssetsFile))
	if err != nil {
		return err
	}
	defer assets.Close()

	err = os.MkdirAll(os.Args[3], 0777)
	if err != nil {
		return err
	}

	pack, err := os.Open(path.Join(os.Args[2], AssetsDataFile))
	if err != nil {
		return err
	}
	defer pack.Close()

	return assets.RangeObjects(func(desc Object, r io.ReadSeeker) error {
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

func MusicPack() error {
	dataRoot := os.Args[2]
	musicDir := os.Args[3]
	outputDir := os.Args[4]

	// Create output dir
	err := os.MkdirAll(outputDir, 0777)
	if err != nil {
		return err
	}

	log.Print("Preparing .resS file...")
	newMap, err := prepareMusic(musicDir, path.Join(outputDir, AssetsDataFile))

	if len(newMap) == 0 {
		return errors.Errorf("suitable tracks not found in %v", musicDir)
	}

	log.Print("Parsing assets file...")
	assets, err := NewAssetsReader(path.Join(dataRoot, AssetsFile))
	if err != nil {
		return err
	}
	defer assets.Close()

	log.Print("Checking existing assets...")
	var replace []ReplacementObject
	var remove []uint32

	used := make(map[string]bool)

	err = assets.RangeObjects(func(desc Object, r io.ReadSeeker) error {
		if desc.TypeID != MusicTypeID {
			return nil
		}

		m, err := ParseMusicDescription(r, assets.Order)
		if err != nil {
			return err
		}

		if m, ok := newMap[m.Name]; ok {
			replace = append(replace, ReplacementObject{
				TargetID: desc.ID,
				CustomObject: CustomObject{
					ClassID: MusicTypeID,
					TypeID:  MusicTypeID,
					Data:    m.Bytes(assets.Order),
				},
			})
			used[m.Name] = true
			log.Printf("replacing %v", m.Name)
		} else {
			remove = append(remove, desc.ID)
			log.Printf("removing %v", m.Name)
		}

		return nil
	})
	if err != nil {
		return err
	}

	var add []CustomObject
	addPos := make(map[string]int)
	for _, m := range newMap {
		if used[m.Name] {
			continue
		}
		add = append(add, CustomObject{
			ClassID: MusicTypeID,
			TypeID:  MusicTypeID,
			Data:    m.Bytes(assets.Order),
		})
		addPos[strings.ToLower(m.Name)] = len(add)
		log.Printf("adding %v", m.Name)
	}

	if len(remove) > 0 {
		return errors.New("clean removing of music is not supported yet, make sure your directory contains replacements for all existing tracks")
	}

	log.Print("Creating modified assets...")
	maxID, err := CreateModifiedAssets(
		path.Join(outputDir, AssetsFile), assets, add, replace, remove,
	)
	if err != nil {
		return err
	}

	log.Printf("Parsing %v...", MainData)
	mainData, err := NewAssetsReader(path.Join(dataRoot, MainData))
	if err != nil {
		return err
	}
	defer mainData.Close()

	var resources ResourceManager
	var resObject ReplacementObject
	err = mainData.RangeObjects(func(desc Object, r io.ReadSeeker) error {
		if desc.TypeID != ResourceManagerTypeID {
			return nil
		}
		resObject.TargetID = desc.ID
		resObject.TypeID = desc.TypeID
		resObject.ClassID = desc.ClassID
		return read(r, &resources, mainData.Order, false)
	})
	if err != nil {
		return err
	}
	if len(resources.Resources) == 0 {
		return errors.New("resources manager data not found")
	}

	log.Printf("Creating modified %v...", MainData)
	for name, pos := range addPos {
		resources.Resources["music/"+name] = ObjectReference{
			// Little extra hardcode... @TODO Extract it from externals
			FileID: 1,
			PathID: maxID - uint32(len(add)-pos),
		}
	}

	var buf bytes.Buffer
	err = write(&buf, resources, mainData.Order, false)
	if err != nil {
		return err
	}
	resObject.Data = buf.Bytes()

	_, err = CreateModifiedAssets(
		path.Join(outputDir, MainData), mainData, nil, []ReplacementObject{resObject}, nil,
	)
	if err != nil {
		return err
	}

	log.Printf("Parsing %v...", MusicLibName)
	lib, err := parseMusicLib(path.Join(dataRoot, MusicLibPath, MusicLibName))
	if err != nil {
		return err
	}

	log.Print("Creating modified music lib...")
	inLib := make(map[string]bool)
	for _, group := range lib.Groups {
		for _, track := range group.Tracks {
			inLib[strings.ToLower(track)] = true
		}
	}

	for _, m := range newMap {
		if !inLib[strings.ToLower(m.Name)] {
			lib.Groups = append(lib.Groups, &class.MusicGroup{
				Name:   m.Name,
				Tracks: []string{m.Name},
			})
			log.Printf("adding %v to lib", m.Name)
		} else {
			log.Printf("%v already in lib", m.Name)
		}
	}

	for name := range inLib {
		found := false
		for n := range newMap {
			if strings.ToLower(n) == name {
				found = true
				break
			}
		}
		if !found {
			log.Printf("%v found in lib, but missing in resources", name)
		}
	}

	sort.Slice(lib.Groups, func(i, j int) bool {
		return lib.Groups[i].Name < lib.Groups[j].Name
	})

	mDir := path.Join(outputDir, MusicLibPath)
	err = os.MkdirAll(mDir, 0777)
	if err != nil {
		return err
	}

	return saveMusicLib(lib, path.Join(mDir, MusicLibName))
}

// Packs music (all .ogg files) in dir into resS file
// and returns map[track_name]->MusicDescription
func prepareMusic(dir, resS string) (map[string]MusicDescription, error) {
	tracks, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	res, err := os.Create(resS)
	if err != nil {
		return nil, err
	}
	defer res.Close()

	ret := make(map[string]MusicDescription)

	for _, f := range tracks {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".ogg") {
			continue
		}

		track, err := os.Open(path.Join(dir, f.Name()))
		if err != nil {
			return nil, err
		}

		offset, err := res.Seek(0, 1)
		if err != nil {
			return nil, err
		}

		size, err := io.Copy(res, track)
		if err != nil {
			return nil, err
		}

		name := strings.TrimSuffix(f.Name(), ".ogg")
		ret[name] = MusicDescription{
			Name:    name,
			Unknown: MagicNumbers,
			Size:    uint32(size),
			Shift:   uint32(offset),
		}
	}

	return ret, res.Close()
}

func ParseMusicLib() error {
	lib, err := parseMusicLib(os.Args[2])
	if err != nil {
		return err
	}
	log.Print(dump(lib))
	return nil
}

func parseMusicLib(path string) (class.MusicLib, error) {
	var ret class.MusicLib

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return ret, err
	}

	err = proto.Unmarshal(data, &ret)
	return ret, err
}

func saveMusicLib(lib class.MusicLib, path string) error {
	data, err := proto.Marshal(&lib)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, data, 0666)
}

func DumpResources() error {
	assets, err := NewAssetsReader(path.Join(os.Args[2], MainData))
	if err != nil {
		return err
	}
	defer assets.Close()

	return assets.RangeObjects(func(desc Object, r io.ReadSeeker) error {
		if desc.TypeID == ResourceManagerTypeID {
			log.Printf("%+v", desc)
			var res ResourceManager
			err := read(r, &res, assets.Order, false)
			if err != nil {
				return err
			}
			log.Print(dump(res))
		}

		return nil
	})
}
