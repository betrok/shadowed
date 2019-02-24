package txtpack

import (
	"fmt"
	"github.com/alecthomas/participle"
	"github.com/alecthomas/participle/lexer"
	"github.com/alecthomas/participle/lexer/ebnf"
	"github.com/pkg/errors"
	"github.com/serenize/snaker"
	"reflect"
	"strconv"
)

type Object struct {
	Entries []Entry `@@*`
}

type Boolean bool
func (b *Boolean) Capture(values []string) error {
	switch values[0] {
	case "true":
		*b = true
	case "false":
		*b = false
	default:
		return errors.Errorf("unexpected value for bool '%v'", values[0])
	}
	return nil
}

type Entry struct {
	Name   string   `@Ident`
	Int    *int     `( ":" @Int`
	Float  *float32 `| ":" @Float`
	Str    *string  `| ":" @String`
	Bool   *Boolean `| ":" @("true" | "false")`
	// enums?
	Ident  *string  `| ":" @Ident`
	Object *Object  `| "{" @@ "}" )`
}

func (e Entry) String() string {
	switch {
	case e.Bool != nil:
		return fmt.Sprintf("<%v(bool): %v>", e.Name, *e.Bool)
	case e.Int != nil:
		return fmt.Sprintf("<%v(int): %v>", e.Name, *e.Int)
	case e.Float != nil:
		return fmt.Sprintf("<%v(float): %v>", e.Name, *e.Float)
	case e.Str != nil:
		return fmt.Sprintf("<%v(str): %v>", e.Name, *e.Str)
	case e.Ident != nil:
		return fmt.Sprintf("<%v(ident): %v>", e.Name, *e.Ident)
	case e.Object != nil:
		return fmt.Sprintf("<%v: %v>", e.Name, *e.Object)
	default:
		return fmt.Sprintf("<%v: <unknown type>>", e.Name)
	}
}

var (
	// Fun fact: whole object can be writen in the single line.
	// New lines are not necessary part of syntax, any whitespace works just fine.
	lex = lexer.Must(ebnf.New(`
		String = "\"" { "\u0000"…"\uffff"-"\""-"\\"-"'" | "\\" any } "\"" .
		Ident = (alpha | "_") { "_" | alpha | digit } .
		Float = [ "-" | "+" ] decimals "." [decimals] [exponent]
				| [ "-" | "+" ] "." decimals [exponent] .
		Int = [ "-" | "+" ] decimals .
		Punct = "{" | ":" | "}" .
		Whitespace = " " | "\t" | "\n" | "\r" .

		alpha = "a"…"z" | "A"…"Z" .
		digit = "0"…"9" .
		decimals = digit { digit } .
		exponent  = ( "e" | "E" ) [ "+" | "-" ] decimals .
		any = "\u0000"…"\uffff" .
	`))
	parser = participle.MustBuild(
		&Object{},
		participle.Lexer(lex),
		participle.Elide("Whitespace"),
		participle.Map(func(token lexer.Token) (lexer.Token, error) {
			var err error
			token.Value, err = unescape(token.Value)
			return token, err
		}, "String"),
	)
)

// Custom unquote: for whatever reason ' is escaped while strings are wrapped in ".
// Besides the editor stores multibyte unicode values as escaped bytes,
// but can load unescaped version just fine...
func unescape(in string) (string, error) {
	// lexer should make sure that this never happens, but just in case...
	if len(in) < 2 || in[0] != '"' || in[len(in) - 1] != '"' {
		return "", errors.New("invalid string literal")
	}

	data := []byte(in[1:len(in)-1])
	cur := 0

	for i := 0; i < len(data); i++ {
		val := data[i]
		if val == '\\' {
			if i == len(data) - 1 {
				return "",  errors.New("backslash on end of string literal")
			}
			next := data[i + 1]
			switch next {
			case '\'', '"', '\\':
				val = next
				i++

			case 'n':
				val = '\n'
				i++

			case 'r':
				val = '\r'
				i++

			case 't':
				val = '\t'
				i++

			case '0', '1', '2', '3', '4', '5', '6', '7':
				// handle escaped byte data, it should be 3 octal digits
				if len(data) - i < 4 {
					return "", errors.New("invalid escaped value")
				}
				t, err := strconv.ParseUint(string(data[i+1: i+4]), 8, 8)
				if err != nil {
					return "", errors.New("invalid escaped value")
				}
				val = byte(t)
				i += 3

			default:
				return "", errors.New("invalid escaped value")
			}

		}
		data[cur] = val
		cur++
	}

	return string(data[:cur]), nil
}

func Parse(data []byte) (Object, error) {
	var ret Object
	err := parser.ParseBytes(data, &ret)
	return ret, err
}

func Unmarshal(data []byte, i interface{}) error {
	parsed, err := Parse(data)
	if err != nil {
		return nil
	}

	val := reflect.ValueOf(i)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return errors.Errorf("unsupported type %v, pointer to structure expected", val.Type())
	}

	return mapObject(parsed, val.Elem(), "")
}

func mapObject(data Object, val reflect.Value, path string) error {
	for _, entry := range data.Entries {
		field := val.FieldByName(snaker.SnakeToCamel(entry.Name))
		path := path + ":" + entry.Name
		if field.IsValid() {
			return errors.Errorf("%v: field not found", path)
		}

		err := mapEntry(entry, field, path)
		if err != nil {
			return err
		}
	}

	return nil
}

func mapEntry(entry Entry, val reflect.Value, path string) error {
	switch val.Kind() {
	case reflect.Bool:
		if entry.Bool == nil {
			return errors.Errorf("%v: incompatible type", path)
		}
		val.SetBool(bool(*entry.Bool))

	case reflect.Int, reflect.Int32, reflect.Int64:
		if entry.Int == nil {
			return errors.Errorf("%v: incompatible type", path)
		}
		val.SetInt(int64(*entry.Int))

	case reflect.Float32, reflect.Float64:
		switch {
		case entry.Int != nil:
			val.SetFloat(float64(*entry.Int))
		case entry.Float != nil:
			val.SetFloat(float64(*entry.Int))
		default:
			return errors.Errorf("%v: incompatible type", path)
		}

	case reflect.String:
		if entry.Str == nil {
			return errors.Errorf("%v: incompatible type", path)
		}
		val.SetString(*entry.Str)

	case reflect.Slice:
		sub := reflect.New(val.Type().Elem())
		err := mapEntry(entry, val, path)
		if err != nil {
			return err
		}
		val.Set(reflect.Append(val, sub))

	case reflect.Struct:
		if entry.Object == nil {
			return errors.Errorf("%v: incompatible type", path)
		}
		err := mapObject(*entry.Object, val, path)
		if err != nil {
			return err
		}

	// @TODO What about identifiers?

	default:
		return errors.Errorf("%v: unsupported type", path)
	}

	return nil
}

