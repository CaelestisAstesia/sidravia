package portable

import (
	"encoding/binary"
	"io"
)

// The producer emits single-disk, comment-free ZIPs with fewer than 20003
// entries and assets below 2 GiB. Bound the central directory BEFORE the stdlib
// parser allocates entries. ZIP64, opaque extras and uncounted headers are not
// part of that distribution contract and require manual installation.
func validateZIPDirectory(r io.ReaderAt, size int64, want int) error {
	if size < 22 || want < 1 || want > 20002 {
		return ErrInvalid
	}
	var footer [22]byte
	if _, err := r.ReadAt(footer[:], size-22); err != nil {
		return err
	}
	u16 := func(at int) uint16 { return binary.LittleEndian.Uint16(footer[at:]) }
	u32 := func(at int) uint32 { return binary.LittleEndian.Uint32(footer[at:]) }
	if u32(0) != 0x06054b50 || u16(4) != 0 || u16(6) != 0 || u16(8) != uint16(want) || u16(10) != uint16(want) || u16(20) != 0 {
		return ErrInvalid
	}
	length, offset := int64(u32(12)), int64(u32(16))
	if length == 0xffff {
		return ErrInvalid
	} // Go 1.26.4 also treats this value as ZIP64
	if length < int64(want)*46 || length > int64(want)*(46+1024+128) || offset+length != size-22 {
		return ErrInvalid
	}
	position := offset
	for n := 0; n < want; n++ {
		var header [46]byte
		if position+46 > offset+length {
			return ErrInvalid
		}
		if _, err := r.ReadAt(header[:], position); err != nil {
			return err
		}
		if binary.LittleEndian.Uint32(header[:]) != 0x02014b50 {
			return ErrInvalid
		}
		name := int64(binary.LittleEndian.Uint16(header[28:]))
		extra := int64(binary.LittleEndian.Uint16(header[30:]))
		comment := binary.LittleEndian.Uint16(header[32:])
		if name < 1 || name > 1024 || extra > 128 || comment != 0 || binary.LittleEndian.Uint16(header[34:]) != 0 {
			return ErrInvalid
		}
		if binary.LittleEndian.Uint32(header[20:]) == 0xffffffff || binary.LittleEndian.Uint32(header[24:]) == 0xffffffff || binary.LittleEndian.Uint32(header[42:]) == 0xffffffff {
			return ErrInvalid
		}
		end := position + 46 + name + extra
		if end > offset+length {
			return ErrInvalid
		}
		// Unknown extra types remain bounded; ZIP64 must not override the ordinary
		// field values after this preflight. No allocation depends on untrusted size.
		for at := position + 46 + name; at < end; {
			var field [4]byte
			if at+4 > end {
				return ErrInvalid
			}
			if _, err := r.ReadAt(field[:], at); err != nil {
				return err
			}
			id := binary.LittleEndian.Uint16(field[:])
			length := int64(binary.LittleEndian.Uint16(field[2:]))
			if id == 1 || at+4+length > end {
				return ErrInvalid
			}
			at += 4 + length
		}
		position = end
	}
	if position != offset+length {
		return ErrInvalid
	}
	return nil
}
