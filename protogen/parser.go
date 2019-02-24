package main

import (
	"bytes"
	"fmt"
	"github.com/betrok/shadowed/txtpack"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unsafe"
)

type Kind string

const (
	Unknown Kind = "unknown"
	Bool    Kind = "bool"
	Int     Kind = "int"
	Float   Kind = "float"
	String  Kind = "string"
	Object  Kind = "object"
	Enum    Kind = "enum"
	// Some .bytes files are not actuality encoded, e.g. misc/hiring.bytes
	Raw Kind = "raw"
)

type Type struct {
	ID       int
	Name     string
	Repeated bool
	Kind     Kind
	// Possible values for enum
	Values map[int]string `json:",omitempty"`
	// Nested fields, only for objects
	Children []Type `json:",omitempty"`
}

func (t Type) FindChild(id int) (Type, bool) {
	for _, c := range t.Children {
		if c.ID == id {
			return c, true
		}
	}
	return Type{}, false
}

func (t *Type) AddChild(n Type) error {
	for i, c := range t.Children {
		if c.ID == n.ID {
			var err error
			n, err = c.Combine(n)
			if err != nil {
				return err
			}
			t.Children[i] = n
			return nil
		}
	}
	t.Children = append(t.Children, n)
	sort.Slice(t.Children, func(i, j int) bool {
		return t.Children[i].ID < t.Children[j].ID
	})
	return nil
}

func (t Type) Combine(n Type) (Type, error) {
	var ret Type
	if t.Name != n.Name {
		return ret, errors.New("types have different names")
	}
	ret.Name = t.Name

	if t.Repeated || n.Repeated {
		ret.Repeated = true
	}

	if t.ID != n.ID {
		return ret, errors.New("types have different ids")
	}
	ret.ID = t.ID

	if t.Kind != n.Kind {
		return ret, errors.New("types are incompatible")
	}
	ret.Kind = t.Kind

	// merge fields
	if ret.Kind == Object {
		for _, child := range append(t.Children, n.Children...) {
			err := ret.AddChild(child)
			if err != nil {
				return ret, err
			}
		}
	}

	if ret.Kind == Enum {
		ret.Values = map[int]string{}
		for k, v := range t.Values {
			ret.Values[k] = v
		}
		for k, v := range n.Values {
			old, ok := ret.Values[k]
			if ok && old != v {
				return ret, errors.Errorf("enum value %v has multiple names: '%v'/'%v", k, old, v)
			}
			ret.Values[k] = v
		}
	}

	return ret, nil
}

func doPack(source, published string, types map[string]Type) error {
	return filepath.Walk(published, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".bytes") {
			return nil
		}

		local, err := filepath.Rel(published, path)
		if err != nil {
			return err
		}

		dir := filepath.Dir(local)
		parts := strings.Split(filepath.Base(local), ".")
		name := strings.Join(parts[:len(parts)-1], ".")

		typeName := parts[len(parts)-2]
		if dir != "." {
			typeName = filepath.ToSlash(dir) + "/" + typeName
		}

		log.Printf("%v -> %v", local, typeName)

		pub, err := ioutil.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%v: %v", path, err)
		}
		// Source name can have different case... Annoying as hell.
		src, err := readFileInsensitive(source, filepath.Join(dir, name+".txt"))
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("failed to find .txt for %v", local)
				return nil
			} else {
				return fmt.Errorf("%v: %v", path, err)
			}
		}

		var typ Type

		if bytes.Compare(pub, src) == 0 {
			log.Printf("'%v' is a raw file", local)
			typ = Type{
				Name: typeName,
				Kind: Raw,
			}
		} else {
			typ, err = extractType(typeName, pub, src)
			if err != nil {
				return err
			}
		}

		old, ok := types[typeName]
		if ok {
			typ, err = typ.Combine(old)
			if err != nil {
				return fmt.Errorf("failed to combine type for %v: %v", typeName, err)
			}
		}

		types[typeName] = typ
		return nil
	})
}

func extractType(name string, pub, src []byte) (Type, error) {
	// Almost all type info can be obtained for src.
	// We can not be sure only about float/int when there is no dot.
	parsed, err := txtpack.Parse(src)
	if err != nil {
		return Type{}, err
	}

	// Prepare top buffer as if it was a message with length
	pBuf := proto.NewBuffer(nil)
	_ = pBuf.EncodeVarint(proto.WireBytes)
	_ = pBuf.EncodeRawBytes(pub)

	ret, err := entryType(txtpack.Entry{
		Name:   name,
		Object: &parsed,
	}, pBuf, "")
	if err != nil {
		return ret, err
	}

	if len(bytesLeft(pBuf)) > 0 {
		return ret, errors.Errorf("%v bytes left unread", len(pBuf.Bytes()))
	}

	return ret, nil
}

// Both txt and protobuf representation seems to have same order of fields,
// and protobuf does not use default values.
// So we can match things relatively easily.
func entryType(entry txtpack.Entry, pBuf *proto.Buffer, path string) (ret Type, err error) {
	path += ":" + entry.Name
	ret.Name = entry.Name

	pKey, err := pBuf.DecodeVarint()
	if err != nil {
		return ret, err
	}

	ret.ID = int(pKey >> 3)
	pType := pKey & 7

	switch {
	case entry.Bool != nil:
		if pType != proto.WireVarint {
			return ret, errors.Errorf("%v: incompatible type bool/%v", path, pType)
		}
		val, err := pBuf.DecodeVarint()
		if err != nil {
			return ret, errors.Errorf("%v: decode: %v", path, err)
		}
		b := bool(*entry.Bool)
		switch {
		case b && val == 1:
		case !b && val == 0:
		default:
			return ret, errors.Errorf("%v: different values: %v/%v", path, b, val)
		}
		ret.Kind = Bool

	case entry.Ident != nil:
		if pType != proto.WireVarint {
			return ret, errors.Errorf("%v: incompatible type indent/%v", path, pType)
		}
		val, err := pBuf.DecodeVarint()
		if err != nil {
			return ret, errors.Errorf("%v: decode: %v", path, err)
		}
		ret.Kind = Enum
		ret.Values = map[int]string{
			int(val): *entry.Ident,
		}

	case entry.Float != nil:
		if pType != proto.WireFixed32 {
			return ret, errors.Errorf("%v: incompatible type float/%v", path, pType)
		}
		val, err := pBuf.DecodeFixed32()
		if err != nil {
			return ret, errors.Errorf("%v: decode: %v", path, err)
		}
		float := math.Float32frombits(uint32(val))
		if *entry.Float != float {
			return ret, errors.Errorf("%v: different values: %v/%v", path, *entry.Float, float)
		}
		ret.Kind = Float

	case entry.Int != nil:
		switch pType {
		case proto.WireVarint:
			val, err := pBuf.DecodeVarint()
			if err != nil {
				return ret, errors.Errorf("%v: decode: %v", path, err)
			}
			if *entry.Int != int(val) {
				return ret, errors.Errorf("%v: different values %v/%v", path, *entry.Int, int(val))
			}
			ret.Kind = Int
		// It can be float as well
		case proto.WireFixed32:
			val, err := pBuf.DecodeFixed32()
			if err != nil {
				return ret, errors.Errorf("%v: decode: %v", path, err)
			}
			float := math.Float32frombits(uint32(val))
			if float32(*entry.Int) != float {
				return ret, errors.Errorf("%v: different values %v/%v", path, *entry.Int, float)
			}
			ret.Kind = Float

		default:
			return ret, errors.Errorf("%v: incompatible type int/%v", path, pType)
		}

	case entry.Str != nil:
		if pType != proto.WireBytes {
			return ret, errors.Errorf("%v: incompatible type string/%v", path, pType)
		}
		str, err := pBuf.DecodeStringBytes()
		if err != nil {
			return ret, errors.Errorf("%v: decode: %v", path, err)
		}
		if *entry.Str != str {
			return ret, errors.Errorf("%v: different values: '%v'/'%v'", path, *entry.Str, str)
		}
		ret.Kind = String

	case entry.Object != nil:
		if pType != proto.WireBytes {
			return ret, errors.Errorf("%v: incompatible type object/%v", path, pType)
		}
		raw, err := pBuf.DecodeRawBytes(false)
		if err != nil {
			return ret, errors.Errorf("%v: decode: %v", path, err)
		}
		childBuf := proto.NewBuffer(raw)
		for i := 0; i < len(entry.Object.Entries); i++ {
			pack, n, err := checkRepeated(i, entry.Object.Entries, childBuf)
			if err != nil {
				return ret, errors.Errorf("%v: check pack: %v", path, err)
			}

			var typ Type
			if n > 0 {
				typ = pack
				i += n - 1
			} else {
				child := entry.Object.Entries[i]

				typ, err = entryType(child, childBuf, path)
				if err != nil {
					return ret, err
				}
			}

			_, ok := ret.FindChild(typ.ID)
			if ok {
				typ.Repeated = true
			}

			err = ret.AddChild(typ)
			if err != nil {
				return ret, err
			}
		}
		if len(bytesLeft(childBuf)) > 0 {
			return ret, errors.Errorf("%v: %v bytes left unread", path, len(pBuf.Bytes()))
		}

		ret.Kind = Object

	default:
		return ret, errors.Errorf("%v: unsupported entry type in %v", path, entry)

	}

	return ret, nil
}

func checkRepeated(i int, entries []txtpack.Entry, topBuf *proto.Buffer) (ret Type, n int, err error) {
	entry := entries[i]
	// Only primitive types can be packed
	if entry.Bool == nil && entry.Int == nil && entry.Float == nil && entry.Ident == nil {
		return ret, 0, nil
	}

	// Pick key
	val, _ := proto.DecodeVarint(bytesLeft(topBuf))
	// Packed repeat should have this wire type
	if val&7 != proto.WireBytes {
		return ret, 0, nil
	}
	ret.ID = int(val >> 3)

	// Read key from top stream as well
	_, _ = topBuf.DecodeVarint()
	pack, err := topBuf.DecodeRawBytes(false)
	if err != nil {
		return ret, 0, errors.Errorf("decode pack: %v", err)
	}

	ret.Name = entry.Name
	ret.Repeated = true

	pBuf := proto.NewBuffer(pack)
	what := entry.Name
	for i+n < len(entries) {
		entry = entries[i+n]
		if entry.Name != what {
			break
		}
		n++

		switch {
		case entry.Bool != nil:
			val, err := pBuf.DecodeVarint()
			if err != nil {
				return ret, 0, errors.Errorf("decode: %v", err)
			}
			b := bool(*entry.Bool)
			switch {
			case b && val == 1:
			case !b && val == 0:
			default:
				return ret, 0, errors.Errorf("%v: different values: %v/%v", b, val)
			}
			ret.Kind = Bool

		case entry.Ident != nil:
			val, err := pBuf.DecodeVarint()
			if err != nil {
				return ret, 0, errors.Errorf("decode: %v", err)
			}
			if ret.Values == nil {
				ret.Kind = Enum
				ret.Values = map[int]string{
					int(val): *entry.Ident,
				}
			} else {
				old, ok := ret.Values[int(val)]
				if ok && old != *entry.Ident {
					return ret, 0, errors.Errorf("enum value %v has multiple names: '%v'/'%v",
						val, old, *entry.Ident)
				}
				ret.Values[int(val)] = *entry.Ident
			}

		case entry.Float != nil:
			val, err := pBuf.DecodeFixed32()
			if err != nil {
				return ret, 0, errors.Errorf("decode: %v", err)
			}
			float := math.Float32frombits(uint32(val))
			if *entry.Float != float {
				return ret, 0, errors.Errorf("different values: %v/%v", *entry.Float, float)
			}
			ret.Kind = Float

		case entry.Int != nil:
			// Here things become even more complicated.
			// We do not know wire format -> can't be sure if this is int or float.
			// It will be easier to guess with known amount of fields, see below

		default:
			return ret, 0, errors.New("unreachable point reached")
		}
	}

	if entry.Int != nil {
		// Check float case first
		isFloat := true
		if n*4 != len(pack) {
			isFloat = false
		} else {
			for j := i; j < i+n; j++ {
				entry := entries[j]
				val, err := pBuf.DecodeFixed32()
				if err != nil {
					isFloat = false
					break
				}
				float := math.Float32frombits(uint32(val))
				if float32(*entry.Int) != float {
					isFloat = false
					break
				}
			}
		}

		if isFloat {
			ret.Kind = Float
		} else {
			// Restore buffer
			pBuf = proto.NewBuffer(pack)
			for j := i; j < i+n; j++ {
				entry := entries[j]
				val, err := pBuf.DecodeVarint()
				if err != nil {
					return ret, 0, errors.Errorf("decode: %v", err)
				}
				if *entry.Int != int(val) {
					return ret, 0, errors.Errorf("different values %v/%v", *entry.Int, int(val))
				}
			}
			ret.Kind = Int
		}
	}

	if len(bytesLeft(pBuf)) != 0 {
		return ret, 0, errors.Errorf("%v bytes left unread in pack", len(pBuf.Bytes()))
	}

	return
}

// Oh well... There is no way to get state from porto.Buffer and I discovered it way too late.
// Hopefully layout will not change any soon...
func bytesLeft(pBuf *proto.Buffer) []byte {
	type buffer struct {
		buf           []byte
		index         int
		deterministic bool
	}
	casted := (*buffer)(unsafe.Pointer(pBuf))

	return casted.buf[casted.index:]
}
