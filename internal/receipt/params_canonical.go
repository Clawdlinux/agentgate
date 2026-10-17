package receipt

import (
	"encoding/json"
	"slices"
	"strconv"
	"unicode/utf16"

	"github.com/gowebpki/jcs"
)

// appendParamsValue only accepts values from the validated params decoder.
func appendParamsValue(dst []byte, value any) []byte {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		slices.SortFunc(keys, func(a, b string) int {
			return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
		})
		dst = append(dst, '{')
		for i, key := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendParamsString(dst, key)
			dst = append(dst, ':')
			dst = appendParamsValue(dst, v[key])
		}
		return append(dst, '}')
	case []any:
		dst = append(dst, '[')
		for i, element := range v {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendParamsValue(dst, element)
		}
		return append(dst, ']')
	case string:
		return appendParamsString(dst, v)
	case json.Number:
		number, _ := strconv.ParseFloat(v.String(), 64) // Already validated as finite.
		canonical, _ := jcs.NumberToJSON(number)
		return append(dst, canonical...)
	case bool:
		return strconv.AppendBool(dst, v)
	default:
		return append(dst, "null"...)
	}
}

func appendParamsString(dst []byte, value string) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '"')
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch c {
		case '"', '\\':
			dst = append(dst, '\\', c)
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\t':
			dst = append(dst, '\\', 't')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\f':
			dst = append(dst, '\\', 'f')
		case '\r':
			dst = append(dst, '\\', 'r')
		default:
			if c < 0x20 {
				dst = append(dst, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
			} else {
				dst = append(dst, c)
			}
		}
	}
	return append(dst, '"')
}
