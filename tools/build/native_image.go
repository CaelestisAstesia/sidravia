package main

import (
	"bytes"
	"debug/elf"
	"debug/pe"
	"encoding/binary"
	"strings"
)

func matchesELF(data []byte, arch string) bool {
	machine := elf.EM_X86_64
	if arch == "arm64" {
		machine = elf.EM_AARCH64
	}
	return len(data) >= 20 && bytes.Equal(data[:6], []byte{0x7f, 'E', 'L', 'F', 2, 1}) &&
		elf.Machine(binary.LittleEndian.Uint16(data[18:20])) == machine
}

func matchesPE(data []byte, arch string) bool {
	if len(data) < 64 || string(data[:2]) != "MZ" {
		return false
	}
	offset := uint64(binary.LittleEndian.Uint32(data[60:64]))
	if offset < 64 || offset+26 > uint64(len(data)) || string(data[offset:offset+4]) != "PE\x00\x00" {
		return false
	}
	machine := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
	if arch == "arm64" {
		machine = pe.IMAGE_FILE_MACHINE_ARM64
	}
	return binary.LittleEndian.Uint16(data[offset+4:offset+6]) == machine &&
		binary.LittleEndian.Uint16(data[offset+24:offset+26]) == 0x20b // PE32+
}

func matchesWindowsGUI(name string, data []byte, arch string) bool {
	switch {
	case strings.HasSuffix(name, ".dll") || strings.HasSuffix(name, ".exe"):
		return matchesPE(data, arch)
	case strings.HasSuffix(name, ".so"):
		// Flutter's Windows AOT snapshots use ELF.
		return matchesELF(data, arch)
	default:
		return true
	}
}
