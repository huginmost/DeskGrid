// Command windows-resources embeds DeskGrid's ICO and version metadata in a
// Windows amd64 COFF resource object. It uses only the Go standard library.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"sort"
	"unicode/utf16"
)

var order = binary.LittleEndian

type resource struct {
	kind, id uint32
	data     []byte
}

func main() {
	icon := flag.String("icon", "internal/branding/deskgrid.ico", "multi-size ICO")
	out := flag.String("out", "deskgrid_windows_amd64.syso", "COFF output")
	flag.Parse()
	if err := generate(*icon, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(icon, out string) error {
	data, err := os.ReadFile(icon)
	if err != nil {
		return err
	}
	if len(data) < 6 || order.Uint16(data) != 0 || order.Uint16(data[2:]) != 1 {
		return fmt.Errorf("invalid ICO header")
	}
	count := int(order.Uint16(data[4:]))
	if count == 0 || len(data) < 6+count*16 {
		return fmt.Errorf("invalid ICO directory")
	}
	group := append([]byte(nil), data[:6]...)
	resources := make([]resource, 0, count+2)
	for i := 0; i < count; i++ {
		entry := data[6+i*16 : 22+i*16]
		length, offset := uint64(order.Uint32(entry[8:])), uint64(order.Uint32(entry[12:]))
		if length == 0 || offset+length > uint64(len(data)) {
			return fmt.Errorf("ICO image %d outside file", i)
		}
		resources = append(resources, resource{3, uint32(i + 1), data[offset : offset+length]})
		group = append(group, entry[:12]...)
		group = binary.LittleEndian.AppendUint16(group, uint16(i+1))
	}
	resources = append(resources, resource{14, 1, group}, resource{16, 1, versionInfo()})
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].kind != resources[j].kind {
			return resources[i].kind < resources[j].kind
		}
		return resources[i].id < resources[j].id
	})
	section, relocations := resourceSection(resources)
	var file bytes.Buffer
	write := func(value any) { _ = binary.Write(&file, order, value) }
	// One read-only .rsrc section and one static section symbol.
	write(uint16(0x8664))
	write(uint16(1))
	write(uint32(0))
	write(uint32(60 + len(section) + 10*len(relocations)))
	write(uint32(1))
	write(uint16(0))
	write(uint16(0))
	file.Write([]byte{'.', 'r', 's', 'r', 'c', 0, 0, 0})
	write(uint32(0))
	write(uint32(0))
	write(uint32(len(section)))
	write(uint32(60))
	write(uint32(60 + len(section)))
	write(uint32(0))
	write(uint16(len(relocations)))
	write(uint16(0))
	write(uint32(0x40000040))
	file.Write(section)
	for _, offset := range relocations {
		write(offset)
		write(uint32(0))
		write(uint16(0x0003)) // IMAGE_REL_AMD64_ADDR32NB
	}
	file.Write([]byte{'.', 'r', 's', 'r', 'c', 0, 0, 0})
	write(uint32(0))
	write(uint16(1))
	write(uint16(0))
	write(uint8(3)) // IMAGE_SYM_CLASS_STATIC
	write(uint8(0))
	write(uint32(4)) // Empty COFF string table.
	if err := os.WriteFile(out, file.Bytes(), 0644); err != nil {
		return err
	}
	fmt.Printf("DeskGrid Windows resources: %d icon sizes + product version metadata\n", count)
	return nil
}

func resourceSection(resources []resource) ([]byte, []uint32) {
	var types []uint32
	for _, r := range resources {
		if len(types) == 0 || types[len(types)-1] != r.kind {
			types = append(types, r.kind)
		}
	}
	data := make([]byte, 16+8*len(types))
	order.PutUint16(data[14:], uint16(len(types)))
	var relocations []uint32
	for i, kind := range types {
		var children []resource
		for _, r := range resources {
			if r.kind == kind {
				children = append(children, r)
			}
		}
		offset := len(data)
		order.PutUint32(data[16+i*8:], kind)
		order.PutUint32(data[20+i*8:], uint32(offset)|0x80000000)
		data = append(data, make([]byte, 16+8*len(children))...)
		order.PutUint16(data[offset+14:], uint16(len(children)))
		for j, r := range children {
			language := len(data)
			order.PutUint32(data[offset+16+j*8:], r.id)
			order.PutUint32(data[offset+20+j*8:], uint32(language)|0x80000000)
			data = append(data, make([]byte, 24)...)
			order.PutUint16(data[language+14:], 1)
			order.PutUint32(data[language+16:], 0x0409)
			entry := len(data)
			order.PutUint32(data[language+20:], uint32(entry))
			data = append(data, make([]byte, 16)...)
			order.PutUint32(data[entry:], uint32(len(data)))
			order.PutUint32(data[entry+4:], uint32(len(r.data)))
			relocations = append(relocations, uint32(entry))
			data = append(data, r.data...)
			for len(data)%4 != 0 {
				data = append(data, 0)
			}
		}
	}
	return data, relocations
}

func wide(s string) []byte {
	var data []byte
	for _, c := range utf16.Encode([]rune(s + "\x00")) {
		data = order.AppendUint16(data, c)
	}
	return data
}

func versionBlock(key string, kind uint16, value []byte, children ...[]byte) []byte {
	data := make([]byte, 6)
	length := len(value)
	if kind == 1 {
		length /= 2
	}
	order.PutUint16(data[2:], uint16(length))
	order.PutUint16(data[4:], kind)
	data = append(data, wide(key)...)
	pad := func() {
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
	}
	pad()
	data = append(data, value...)
	for _, child := range children {
		pad()
		data = append(data, child...)
	}
	order.PutUint16(data, uint16(len(data)))
	return data
}

func versionInfo() []byte {
	var fixed []byte
	for _, value := range []uint32{0xfeef04bd, 0x10000, 0x10000, 0x20000, 0x10000, 0x20000, 0x3f, 0, 0x40004, 1, 0, 0, 0} {
		fixed = order.AppendUint32(fixed, value)
	}
	var strings [][]byte
	for _, pair := range [][2]string{{"CompanyName", "DeskGrid"}, {"FileDescription", "DeskGrid 桌面整理工具"}, {"FileVersion", "1.0.2.0"}, {"InternalName", "DeskGrid"}, {"OriginalFilename", "DeskGrid.exe"}, {"ProductName", "DeskGrid"}, {"ProductVersion", "1.0.2.0"}} {
		strings = append(strings, versionBlock(pair[0], 1, wide(pair[1])))
	}
	table := versionBlock("040904B0", 1, nil, strings...)
	info := versionBlock("StringFileInfo", 1, nil, table)
	translation := versionBlock("Translation", 0, []byte{0x09, 0x04, 0xb0, 0x04})
	vars := versionBlock("VarFileInfo", 1, nil, translation)
	return versionBlock("VS_VERSION_INFO", 0, fixed, info, vars)
}
