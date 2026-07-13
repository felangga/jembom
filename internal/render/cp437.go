package render

import (
	"io"
	"strings"
)

// Mode selects the terminal encoding for a client connection.
type Mode uint8

const (
	ModeUnicode Mode = iota // UTF-8 / Unicode (modern terminals)
	ModeCP437               // IBM CP437 / DOS (BBS, DOSBox, old IBM PC)
	ModeASCII               // 7-bit ASCII fallback (plain-text-only terminals)
)

// cp437Replacer converts Unicode box/block characters to single-byte CP437 values.
// ANSI escape sequences are untouched (all bytes < 0x80, no overlap).
var cp437Replacer = strings.NewReplacer(
	// Block elements
	"█", "\xdb",
	"▓", "\xb2",
	"▒", "\xb1",
	"░", "\xb0",
	"▄", "\xdc",
	"▀", "\xdf",
	"▌", "\xdd",
	"▐", "\xde",
	"■", "\xfe",
	// Arrows
	"►", "\x10",
	"◄", "\x11",
	"▲", "\x1e",
	"▼", "\x1f",
	// Double-line box drawing
	"║", "\xba",
	"═", "\xcd",
	"╔", "\xc9",
	"╗", "\xbb",
	"╚", "\xc8",
	"╝", "\xbc",
	"╠", "\xcc",
	"╣", "\xb9",
	"╦", "\xcb",
	"╩", "\xca",
	"╬", "\xce",
	"╡", "\xb5",
	"╢", "\xb6",
	"╖", "\xb7",
	"╕", "\xb8",
	"╜", "\xbd",
	"╛", "\xbe",
	"╞", "\xc6",
	"╟", "\xc7",
	"╧", "\xcf",
	"╨", "\xd0",
	"╤", "\xd1",
	"╥", "\xd2",
	"╙", "\xd3",
	"╘", "\xd4",
	"╒", "\xd5",
	"╓", "\xd6",
	"╫", "\xd7",
	"╪", "\xd8",
	// Single-line box drawing
	"│", "\xb3",
	"┤", "\xb4",
	"┐", "\xbf",
	"└", "\xc0",
	"┴", "\xc1",
	"┬", "\xc2",
	"├", "\xc3",
	"─", "\xc4",
	"┼", "\xc5",
	"┘", "\xd9",
	"┌", "\xda",
)

// CP437Writer wraps an io.Writer and translates Unicode box/block characters
// to their CP437 single-byte equivalents before writing.
// All other bytes (ANSI escapes, ASCII text) are passed through unchanged.
type CP437Writer struct {
	W io.Writer
}

func (c *CP437Writer) Write(p []byte) (int, error) {
	_, err := c.W.Write([]byte(cp437Replacer.Replace(string(p))))
	if err != nil {
		return 0, err
	}
	return len(p), nil // satisfy io.Writer contract
}
