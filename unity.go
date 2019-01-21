package main

import (
	"bytes"
	"encoding/binary"
	"github.com/pkg/errors"
	"io"
	"log"
	"os"
	"reflect"
	"strings"
)

const (
	// All the actual shadowrun builds seem to have version 9 of asset file.
	// This implementation consider compatible versions only.
	VersionMin = 9
	VersionMax = 9
)

/* See https://github.com/HearthSim/UnityPack/wiki/Format-Documentation for unity assets format specs.
In general it's

struct {
	Header
	MetaData
	ObjectsBytes
}
*/

type Header struct {
	MetaSize   uint32
	FileSize   uint32
	Version    uint32
	DataOffset uint32
	ByteOrder  uint8
	// padding
	Reserved [3]uint8
}

type MetaData struct {
	TypeInfo  TypesHeader
	Objects   []Object
	Externals []External
}

type TypesHeader struct {
	Signature uint64
	Platform  uint32
	Classes   []Class
	Unknown   uint32
}

type Class struct {
	ID   uint32
	Info TypeInfo
}

type TypeInfo struct {
	Type     string
	Name     string
	Size     uint32
	Index    uint32
	IsArray  uint32
	Version  uint32
	Flags    uint32
	Children []TypeInfo
}

type Object struct {
	ID        uint32
	Shift     uint32
	Size      uint32
	TypeID    uint32
	ClassID   uint16
	Destroyed uint16
}

type External struct {
	AssetPath string
	GUID      [16]byte
	Type      uint32
	FilePath  string
}

type AssetsReader struct {
	fd *os.File

	Order    binary.ByteOrder
	Header   Header
	MetaData MetaData
}

func NewAssetsReader(file string) (*AssetsReader, error) {
	var (
		err error
		ret AssetsReader
	)
	ret.fd, err = os.Open(file)
	if err != nil {
		return nil, err
	}

	success := false
	defer func() {
		if !success {
			ret.fd.Close()
		}
	}()

	err = ret.read(&ret.Header, binary.BigEndian)
	if err != nil {
		return nil, err
	}

	if ret.Header.Version < VersionMin || ret.Header.Version > VersionMax {
		return nil, errors.Errorf("unsupported assets file version:\n %v", dump(ret.Header))
	}

	if ret.Header.ByteOrder == 0 {
		ret.Order = binary.LittleEndian
	} else {
		ret.Order = binary.BigEndian
	}

	err = ret.read(&ret.MetaData, ret.Order)

	success = true
	return &ret, nil
}

func (r *AssetsReader) Close() error {
	return r.fd.Close()
}

func (r AssetsReader) Signature() string {
	buf := make([]byte, 8)
	r.Order.PutUint64(buf, r.MetaData.TypeInfo.Signature)
	return strings.TrimRight(string(buf), "\x00")
}

func (r *AssetsReader) RangeObjects(f func(desc Object, r io.ReadSeeker) error) error {
	for _, obj := range r.MetaData.Objects {
		err := f(obj, io.NewSectionReader(r.fd, int64(r.Header.DataOffset+obj.Shift), int64(obj.Size)))
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *AssetsReader) read(i interface{}, order binary.ByteOrder) error {
	return read(r.fd, i, order, true)
}

func read(r io.ReadSeeker, i interface{}, order binary.ByteOrder, cString bool) error {
	val := reflect.ValueOf(i)
	if val.Kind() != reflect.Ptr {
		return errors.Errorf("unsupported type %v, pointer expected", val.Type())
	}
	return readVal(r, val.Elem(), order, cString)
}

func readVal(r io.ReadSeeker, val reflect.Value, order binary.ByteOrder, cString bool) error {
	switch val.Kind() {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return binary.Read(r, order, val.Addr().Interface())

	case reflect.Struct:
		for i := 0; i < val.NumField(); i++ {
			err := readVal(r, val.Field(i), order, cString)
			if err != nil {
				return err
			}
		}

	case reflect.Slice:
		var usize uint32
		err := binary.Read(r, order, &usize)
		if err != nil {
			return err
		}

		slice := reflect.MakeSlice(val.Type(), int(usize), int(usize))
		val.Set(slice)

		fallthrough

	case reflect.Array:
		for i := 0; i < val.Len(); i++ {
			err := readVal(r, val.Index(i), order, cString)
			if err != nil {
				return err
			}
		}

	case reflect.Map:
		var usize uint32
		err := binary.Read(r, order, &usize)
		if err != nil {
			return err
		}

		val.Set(reflect.MakeMap(val.Type()))
		keyType := val.Type().Key()
		elemType := val.Type().Elem()

		for i := uint32(0); i < usize; i++ {
			key := reflect.New(keyType).Elem()
			elem := reflect.New(elemType).Elem()

			err = readVal(r, key, order, cString)
			if err != nil {
				return err
			}
			err = readVal(r, elem, order, cString)
			if err != nil {
				return err
			}

			old := val.MapIndex(key)
			if old.IsValid() {
				log.Printf("[warn] duplicate value for key %v: new: %+v, old: %+v", key.Interface(), elem.Interface(), old.Interface())
			}

			val.SetMapIndex(key, elem)
		}

	case reflect.String:
		if cString {
			pos, err := r.Seek(0, 1)
			if err != nil {
				return err
			}

			var str string
			buf := make([]byte, 100)
			for {
				n, err := r.Read(buf)
				if err != nil {
					return err
				}
				idx := bytes.IndexByte(buf[:n], 0)
				if idx != -1 {
					str += string(buf[:idx])
					break
				}
				str += string(buf[:n])
			}

			pos, err = r.Seek(pos+int64(len(str)+1), 0)
			if err != nil {
				return err
			}
			val.Set(reflect.ValueOf(str))
		} else {
			var usize uint32
			err := binary.Read(r, order, &usize)
			if err != nil {
				return err
			}

			buf := make([]byte, align(usize, 4))
			_, err = io.ReadFull(r, buf)
			if err != nil {
				return err
			}
			val.Set(reflect.ValueOf(string(buf[:usize])))
		}

	default:
		return errors.Errorf("unsupported type %v", val.Type())
	}

	return nil
}

func write(w io.Writer, i interface{}, order binary.ByteOrder, cString bool) error {
	val := reflect.ValueOf(i)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	return writeVal(w, val, order, cString)
}

func writeVal(w io.Writer, val reflect.Value, order binary.ByteOrder, cString bool) error {
	switch val.Kind() {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return binary.Write(w, order, val.Interface())

	case reflect.Struct:
		for i := 0; i < val.NumField(); i++ {
			err := writeVal(w, val.Field(i), order, cString)
			if err != nil {
				return err
			}
		}

	case reflect.Slice:
		err := binary.Write(w, order, uint32(val.Len()))
		if err != nil {
			return err
		}

		fallthrough

	case reflect.Array:
		for i := 0; i < val.Len(); i++ {
			err := writeVal(w, val.Index(i), order, cString)
			if err != nil {
				return err
			}
		}

	case reflect.Map:
		mapLen := uint32(val.Len())
		err := binary.Write(w, order, mapLen)
		if err != nil {
			return err
		}

		for _, key := range val.MapKeys() {
			err = writeVal(w, key, order, cString)
			if err != nil {
				return err
			}

			err = writeVal(w, val.MapIndex(key), order, cString)
			if err != nil {
				return err
			}
		}

	case reflect.String:
		if cString {
			_, err := w.Write(append([]byte(val.String()), 0))
			if err != nil {
				return err
			}
		} else {
			err := binary.Write(w, order, uint32(val.Len()))
			if err != nil {
				return err
			}
			size, err := w.Write([]byte(val.String()))
			if err != nil {
				return err
			}
			return writeAlign(w, size, 4)
		}

	default:
		return errors.Errorf("unsupported type %v", val.Type())
	}

	return nil
}

type CustomObject struct {
	TypeID  uint32
	ClassID uint16
	Data    []byte
}

type ReplacementObject struct {
	CustomObject
	TargetID uint32
}

func CreateModifiedAssets(
	path string, src *AssetsReader,
	add []CustomObject, replace []ReplacementObject, remove []uint32,
) (maxID uint32, err error) {
	deleteMap := make(map[uint32]struct{})
	for _, del := range remove {
		deleteMap[del] = struct{}{}
	}

	replaceMap := make(map[uint32]CustomObject)
	for _, rep := range replace {
		if _, ok := deleteMap[rep.TargetID]; ok {
			return 0, errors.Errorf("targetID %v is presented in both replace and delete lists", rep.TargetID)
		}
		replaceMap[rep.TargetID] = rep.CustomObject
	}

	fd, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer fd.Close()

	// Copy header
	header := src.Header

	// Copy meta. We will modify the objects list only, everything else can be shared
	meta := src.MetaData
	meta.Objects = make([]Object, 0, len(meta.Objects)+len(add)-len(remove))

	var dataSize uint32 = 0
	for _, obj := range src.MetaData.Objects {
		// ignore deleted object
		if _, ok := deleteMap[obj.ID]; ok {
			continue
		}

		if rep, ok := replaceMap[obj.ID]; ok {
			obj.TypeID = rep.TypeID
			obj.ClassID = rep.ClassID
			obj.Size = uint32(len(rep.Data))
		}

		// @TODO align?
		obj.Shift = dataSize
		meta.Objects = append(meta.Objects, obj)
		dataSize += align(uint32(obj.Size), 8)
		if obj.ID > maxID {
			maxID = obj.ID
		}
	}

	for _, obj := range add {
		maxID++
		meta.Objects = append(meta.Objects, Object{
			ID:      maxID,
			Shift:   dataSize,
			Size:    uint32(len(obj.Data)),
			TypeID:  obj.TypeID,
			ClassID: obj.ClassID,
		})
		dataSize += align(uint32(len(obj.Data)), 8)
	}

	// Just to reserve enough space, we will revisit it later
	err = write(fd, header, binary.BigEndian, true)
	if err != nil {
		return 0, err
	}

	metaOffset, err := fd.Seek(0, 1)
	if err != nil {
		return 0, err
	}

	var order binary.ByteOrder = binary.LittleEndian
	if header.ByteOrder != 0 {
		order = binary.BigEndian
	}

	err = write(fd, meta, order, true)
	if err != nil {
		return 0, err
	}

	dataOffset, err := fd.Seek(0, 1)
	if err != nil {
		return 0, err
	}

	header.MetaSize = uint32(dataOffset - metaOffset)
	header.DataOffset = align(uint32(dataOffset), 8)
	header.FileSize = header.DataOffset + dataSize

	_, err = fd.Seek(0, 0)
	if err != nil {
		return 0, err
	}

	// This time with real values
	err = write(fd, header, binary.BigEndian, true)
	if err != nil {
		return 0, err
	}

	_, err = fd.Seek(int64(header.DataOffset), 0)
	if err != nil {
		return 0, err
	}

	err = src.RangeObjects(func(obj Object, r io.ReadSeeker) error {
		if _, ok := deleteMap[obj.ID]; ok {
			return nil
		}

		var err error
		var size int
		if rep, ok := replaceMap[obj.ID]; ok {
			size, err = fd.Write(rep.Data)
		} else {
			// awesome std lib, huh
			var veryConsistentTypes int64
			veryConsistentTypes, err = io.Copy(fd, r)
			size = int(veryConsistentTypes)
		}
		if err != nil {
			return err
		}

		return writeAlign(fd, size, 8)
	})
	if err != nil {
		return 0, err
	}

	for _, obj := range add {
		size, err := fd.Write(obj.Data)
		if err != nil {
			return 0, err
		}
		err = writeAlign(fd, size, 8)
		if err != nil {
			return 0, err
		}
	}

	return maxID, fd.Close()
}

func writeAlign(w io.Writer, size, line int) error {
	if size%line == 0 {
		return nil
	}
	_, err := w.Write(make([]byte, line-size%line))
	return err
}

func align(raw, line uint32) uint32 {
	return (raw + line - 1) / line * line
}

type ObjectReference struct {
	FileID uint32
	PathID uint32
}

type NamedReference struct {
	Name   string
	Object ObjectReference
}

type ResourceManager struct {
	Resources []NamedReference
	Dependent []struct {
		Object       ObjectReference
		Dependencies []ObjectReference
	}
}
