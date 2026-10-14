// Package enc resolves configured encoding names to text encodings.
package enc

import (
	"fmt"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
)

// Lookup returns the encoding for name (case-insensitive). Empty means utf-8. "nop" returns
// encoding.Nop so callers can detect it and keep raw bytes. Other names are resolved by IANA name.
func Lookup(name string) (encoding.Encoding, error) {
	switch n := strings.ToLower(strings.TrimSpace(name)); n {
	case "", "utf-8", "utf8":
		return unicode.UTF8, nil
	case "nop":
		return encoding.Nop, nil
	case "utf-16le":
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM), nil
	case "utf-16be":
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM), nil
	default:
		e, err := ianaindex.IANA.Encoding(n)
		if err != nil || e == nil {
			return nil, fmt.Errorf("unsupported encoding %q", name)
		}
		return e, nil
	}
}
