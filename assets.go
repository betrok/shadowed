package main

import (
	"bytes"
	"encoding/binary"
	"github.com/pkg/errors"
	"io"
	"os"
	"reflect"
	"strings"
)

const (
	// All the actual shadowrun builds seem to have version 9 of asset file.
	// This implementation consider compatible versions only.
	VersionMin = 9
	VersionMax = 13
)

/* See https://github.com/ata4/disunity/wiki/Serialized-file-format for unity assets format specs.
In general it's

struct {
	Header
	MetaData
	// Looks like here is extra 16 zero bytes between the meta data and the objects
	ObjectsData
}
*/

type Header struct {
	MetaSize   uint32
	FileSize   uint32
	Version    uint32
	DataOffset uint32
	ByteOrder  uint8
	// in fact looks like just a padding
	Reserved [3]uint8
}

type MetaData struct {
	TypeInfo  TypesHeader
	Objects   []Object
	Externals []External
}

type TypesHeader struct {
	Signature uint64
	Flags     uint32
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

	err = ret.read(binary.BigEndian, &ret.Header)
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

	err = ret.read(ret.Order, &ret.MetaData)

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

func (r *AssetsReader) RangeObjects(f func(desc Object, r io.Reader) error) error {
	for _, obj := range r.MetaData.Objects {
		_, err := r.fd.Seek(int64(r.Header.DataOffset+obj.Shift), 0)
		if err != nil {
			return err
		}
		err = f(obj, io.LimitReader(r.fd, int64(obj.Size)))
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *AssetsReader) read(byteOrder binary.ByteOrder, i interface{}) error {
	val := reflect.ValueOf(i)
	if val.Kind() != reflect.Ptr {
		return errors.Errorf("unsupported type %v, pointer expected", val.Type())
	}
	return r.readVal(byteOrder, val.Elem())
}

func (r *AssetsReader) readVal(byteOrder binary.ByteOrder, val reflect.Value) error {
	switch val.Kind() {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return binary.Read(r.fd, byteOrder, val.Addr().Interface())

	case reflect.Struct:
		for i := 0; i < val.NumField(); i++ {
			filed := val.Field(i)
			// most of fields can be read just by recursive readVal call
			if filed.Kind() != reflect.Slice {
				err := r.readVal(byteOrder, filed)
				if err != nil {
					return err
				}
				continue
			}
			// for slice we should find it's size
			var usize uint32
			err := binary.Read(r.fd, byteOrder, &usize)
			if err != nil {
				return err
			}
			size := int(usize)

			slice := reflect.MakeSlice(filed.Type(), size, size)
			for j := 0; j < size; j++ {
				err := r.readVal(byteOrder, slice.Index(j))
				if err != nil {
					return err
				}
			}
			filed.Set(slice)
		}

	case reflect.Array:
		for i := 0; i < val.Len(); i++ {
			err := r.readVal(byteOrder, val.Index(i))
			if err != nil {
				return err
			}
		}

	case reflect.String:
		pos, err := r.fd.Seek(0, 1)
		if err != nil {
			return err
		}

		var str string
		buf := make([]byte, 100)
		for {
			n, err := r.fd.Read(buf)
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

		pos, err = r.fd.Seek(pos+int64(len(str)+1), 0)
		if err != nil {
			return err
		}
		val.Set(reflect.ValueOf(str))

	default:
		return errors.Errorf("unsupported type %v", val.Type())
	}

	return nil
}
