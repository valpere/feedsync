// Package export writes marketplace price lists as streaming XML.
package export

import (
	"bytes"
	"encoding/xml"
	"strings"
	"unicode/utf8"
)

// clean drops characters XML 1.0 cannot carry (ASCII 0-31 except tab, LF, CR)
// and invalid UTF-8, as the Rozetka requirements demand.
func clean(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 32 && r != 9 && r != 10 && r != 13 || r == utf8.RuneError }) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r < 32 && r != 9 && r != 10 && r != 13 || r == utf8.RuneError {
			return -1
		}
		return r
	}, s)
}

// Esc escapes text for element content and attribute values.
func Esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(clean(s)))
	return b.String()
}

// cdata wraps s in a CDATA section, splitting any "]]>" so it stays well-formed.
func cdata(s string) string {
	s = strings.ReplaceAll(clean(s), "]]>", "]]]]><![CDATA[>")
	return "<![CDATA[" + s + "]]>"
}
