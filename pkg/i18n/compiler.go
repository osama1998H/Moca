package i18n

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

// GNU MO file format constants.
const (
	moMagic    = 0x950412de // little-endian magic number
	moRevision = 0          // format revision
)

// moHeader is the fixed 28-byte header of a .mo file.
type moHeader struct {
	Magic          uint32
	Revision       uint32
	NumStrings     uint32
	OrigTableOff   uint32
	TransTableOff  uint32
	HashTableSize  uint32
	HashTableOff   uint32
}

// moStringEntry is an offset/length pair in the string table.
type moStringEntry struct {
	Length uint32
	Offset uint32
}

// CompileMO writes translations in GNU MO binary format.
// The hash table is omitted (size=0) which is spec-compliant.
// Translations are sorted by original string for deterministic output.
func CompileMO(translations []Translation, w io.Writer) error {
	if len(translations) == 0 {
		return writeMOEmpty(w)
	}

	// Sort by source text for deterministic ordering and binary search.
	sorted := make([]Translation, len(translations))
	copy(sorted, translations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].SourceText < sorted[j].SourceText
	})

	n := uint32(len(sorted))

	// Build string data: originals and translations.
	var originals, translated [][]byte
	for _, t := range sorted {
		// MO format uses msgctxt + EOT + msgid for context.
		orig := []byte(t.SourceText)
		if t.Context != "" {
			orig = append([]byte(t.Context), 0x04) // EOT separator
			orig = append(orig, []byte(t.SourceText)...)
		}
		originals = append(originals, orig)
		translated = append(translated, []byte(t.TranslatedText))
	}

	// Calculate offsets.
	// Header: 28 bytes
	// Original table: n * 8 bytes
	// Translation table: n * 8 bytes
	// String data follows.
	headerSize := uint32(28)
	tableEntrySize := uint32(8) // 4 bytes length + 4 bytes offset
	origTableOff := headerSize
	transTableOff := origTableOff + n*tableEntrySize

	// String data starts after both tables.
	dataOffset := transTableOff + n*tableEntrySize

	// Build the tables and collect string data.
	var buf bytes.Buffer

	// Write header.
	hdr := moHeader{
		Magic:          moMagic,
		Revision:       moRevision,
		NumStrings:     n,
		OrigTableOff:   origTableOff,
		TransTableOff:  transTableOff,
		HashTableSize:  0,
		HashTableOff:   0,
	}
	if err := binary.Write(&buf, binary.LittleEndian, &hdr); err != nil {
		return fmt.Errorf("mo compile: write header: %w", err)
	}

	// Compute string offsets.
	origEntries := make([]moStringEntry, n)
	transEntries := make([]moStringEntry, n)
	off := dataOffset

	for i := uint32(0); i < n; i++ {
		origEntries[i] = moStringEntry{Length: uint32(len(originals[i])), Offset: off}
		off += uint32(len(originals[i])) + 1 // +1 for NUL terminator
	}
	for i := uint32(0); i < n; i++ {
		transEntries[i] = moStringEntry{Length: uint32(len(translated[i])), Offset: off}
		off += uint32(len(translated[i])) + 1
	}

	// Write original string table.
	for _, e := range origEntries {
		if err := binary.Write(&buf, binary.LittleEndian, &e); err != nil {
			return fmt.Errorf("mo compile: write orig table: %w", err)
		}
	}

	// Write translation string table.
	for _, e := range transEntries {
		if err := binary.Write(&buf, binary.LittleEndian, &e); err != nil {
			return fmt.Errorf("mo compile: write trans table: %w", err)
		}
	}

	// Write string data (originals then translations, each NUL-terminated).
	for _, s := range originals {
		buf.Write(s)
		buf.WriteByte(0)
	}
	for _, s := range translated {
		buf.Write(s)
		buf.WriteByte(0)
	}

	_, err := w.Write(buf.Bytes())
	return err
}

// writeMOEmpty writes a valid but empty MO file.
func writeMOEmpty(w io.Writer) error {
	hdr := moHeader{
		Magic:    moMagic,
		Revision: moRevision,
	}
	return binary.Write(w, binary.LittleEndian, &hdr)
}

// LoadMO reads a GNU MO binary file and returns a map of source→translated strings.
func LoadMO(r io.Reader) (map[string]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("mo load: read: %w", err)
	}

	if len(data) < 28 {
		return nil, fmt.Errorf("mo load: file too small (%d bytes)", len(data))
	}

	// Read header.
	var hdr moHeader
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &hdr); err != nil {
		return nil, fmt.Errorf("mo load: read header: %w", err)
	}

	if hdr.Magic != moMagic {
		return nil, fmt.Errorf("mo load: invalid magic 0x%08x (expected 0x%08x)", hdr.Magic, moMagic)
	}

	n := hdr.NumStrings
	if n == 0 {
		return make(map[string]string), nil
	}

	result := make(map[string]string, n)

	for i := uint32(0); i < n; i++ {
		origOff := hdr.OrigTableOff + i*8
		transOff := hdr.TransTableOff + i*8

		if origOff+8 > uint32(len(data)) || transOff+8 > uint32(len(data)) {
			return nil, fmt.Errorf("mo load: table offset out of bounds at entry %d", i)
		}

		origEntry := readMOEntry(data, origOff)
		transEntry := readMOEntry(data, transOff)

		origStr, err := readMOString(data, origEntry)
		if err != nil {
			return nil, fmt.Errorf("mo load: orig string %d: %w", i, err)
		}
		transStr, err := readMOString(data, transEntry)
		if err != nil {
			return nil, fmt.Errorf("mo load: trans string %d: %w", i, err)
		}

		// Handle context: MO format uses EOT (0x04) to separate context from msgid.
		key := origStr
		if idx := bytes.IndexByte([]byte(key), 0x04); idx >= 0 {
			key = key[idx+1:] // strip context prefix
		}

		result[key] = transStr
	}

	return result, nil
}

func readMOEntry(data []byte, off uint32) moStringEntry {
	return moStringEntry{
		Length: binary.LittleEndian.Uint32(data[off:]),
		Offset: binary.LittleEndian.Uint32(data[off+4:]),
	}
}

func readMOString(data []byte, e moStringEntry) (string, error) {
	end := e.Offset + e.Length
	if end > uint32(len(data)) {
		return "", fmt.Errorf("string offset out of bounds: %d+%d > %d", e.Offset, e.Length, len(data))
	}
	return string(data[e.Offset:end]), nil
}
