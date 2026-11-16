package portable

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"
)

func TestZIPDirectoryBoundsBeforeParsing(t *testing.T) {
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	file, err := writer.Create("file")
	if err != nil {
		t.Fatal(err)
	}
	file.Write([]byte("value"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	raw := data.Bytes()
	if err := validateZIPDirectory(bytes.NewReader(raw), int64(len(raw)), 1); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"count", "zip64-count", "central-size", "zip64-offset", "extra-header"} {
		t.Run(mode, func(t *testing.T) {
			changed := append([]byte(nil), raw...)
			footer := changed[len(changed)-22:]
			switch mode {
			case "count":
				binary.LittleEndian.PutUint16(footer[8:], 0)
				binary.LittleEndian.PutUint16(footer[10:], 0)
			case "zip64-count":
				binary.LittleEndian.PutUint16(footer[8:], 65535)
				binary.LittleEndian.PutUint16(footer[10:], 65535)
			case "central-size":
				binary.LittleEndian.PutUint32(footer[12:], 0xffffffff)
			case "zip64-offset":
				binary.LittleEndian.PutUint32(footer[16:], 0xffffffff)
			case "extra-header":
				binary.LittleEndian.PutUint32(footer[12:], binary.LittleEndian.Uint32(footer[12:])+46)
			}
			if err := validateZIPDirectory(bytes.NewReader(changed), int64(len(changed)), 1); err == nil {
				t.Fatal("unbounded directory accepted")
			}
		})
	}
}

func TestZIPRejectsSDKZIP64DirectorySizeTrigger(t *testing.T) {
	var central bytes.Buffer
	for n := 0; n < 100; n++ {
		size := 609
		if n < 35 {
			size++
		}
		header := make([]byte, 46)
		binary.LittleEndian.PutUint32(header, 0x02014b50)
		binary.LittleEndian.PutUint16(header[28:], uint16(size))
		central.Write(header)
		central.Write(make([]byte, size))
	}
	if central.Len() != 65535 {
		t.Fatal("invalid boundary fixture")
	}
	footer := make([]byte, 22)
	binary.LittleEndian.PutUint32(footer, 0x06054b50)
	binary.LittleEndian.PutUint16(footer[8:], 100)
	binary.LittleEndian.PutUint16(footer[10:], 100)
	binary.LittleEndian.PutUint32(footer[12:], 65535)
	central.Write(footer)
	if err := validateZIPDirectory(bytes.NewReader(central.Bytes()), int64(central.Len()), 100); err == nil {
		t.Fatal("stdlib ZIP64 trigger bypassed directory bound")
	}
}
