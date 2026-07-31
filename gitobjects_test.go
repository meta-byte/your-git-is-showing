package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

const (
	shaA = "1111111111111111111111111111111111111111"
	shaB = "2222222222222222222222222222222222222222"
	shaC = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
)

func zlibBytes(data []byte) []byte {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write(data)
	_ = w.Close()
	return buf.Bytes()
}

func TestParseObjType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want objType
	}{
		{"commit", "commit", objCommit},
		{"tree", "tree", objTree},
		{"blob", "blob", objBlob},
		{"tag", "tag", objTag},
		{"empty", "", objInvalid},
		{"unknown", "wat", objInvalid},
		{"trailing space", "commit ", objInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseObjType(tt.in); got != tt.want {
				t.Errorf("parseObjType(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRefsFromCommit(t *testing.T) {
	valid := "tree " + shaC + "\nparent " + shaA + "\nparent " + shaB + "\n\nauthor A <a@b> 1 +0000\n"
	tests := []struct {
		name string
		in   []byte
		want []string
	}{
		{"tree and parents", []byte(valid), []string{shaC, shaA, shaB}},
		{"stops at blank line", []byte("tree " + shaA + "\n\nparent " + shaB + "\n"), []string{shaA}},
		{"short sha ignored", []byte("tree 1234\n\n"), nil},
		{"no matching field", []byte("committer A <a@b> 1 +0000\n"), nil},
		{"single field line", []byte("tree\n"), nil},
		{"empty", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := refsFromCommit(tt.in)
			if err != nil {
				t.Fatalf("refsFromCommit() error = %v", err)
			}
			if !equalStrings(got, tt.want) {
				t.Errorf("refsFromCommit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRefsFromTree(t *testing.T) {
	entry := func(mode, name string, sha []byte) []byte {
		var b bytes.Buffer
		b.WriteString(mode)
		b.WriteByte(' ')
		b.WriteString(name)
		b.WriteByte(0)
		b.Write(sha)
		return b.Bytes()
	}
	shaBytes := func(s string) []byte {
		b, _ := hex.DecodeString(s)
		return b
	}

	tests := []struct {
		name string
		in   []byte
		want []string
	}{
		{"single entry", entry("100644", "a.txt", shaBytes(shaA)), []string{shaA}},
		{"multiple entries", append(entry("100644", "a.txt", shaBytes(shaA)), entry("40000", "sub", shaBytes(shaB))...), []string{shaA, shaB}},
		{"missing nul", []byte("100644 a.txt" + string(shaBytes(shaA)[:19])), nil},
		{"short sha", append([]byte("100644 a.txt\x00"), shaBytes(shaA)[:19]...), nil},
		{"no space", []byte("junkdata"), nil},
		{"empty", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := refsFromTree(tt.in); !equalStrings(got, tt.want) {
				t.Errorf("refsFromTree() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadLooseObject(t *testing.T) {
	commitData := []byte("tree " + shaC + "\n\nauthor A <a@b> 1 +0000\n")
	tests := []struct {
		name    string
		in      []byte
		wantTyp objType
		wantErr bool
	}{
		{"valid commit", zlibBytes(append([]byte("commit 57\x00"), commitData...)), objCommit, false},
		{"valid blob", zlibBytes([]byte("blob 4\x00data")), objBlob, false},
		{"unknown type", zlibBytes([]byte("wat 0\x00x")), objInvalid, true},
		{"no header nul", zlibBytes([]byte("no nul here")), objInvalid, true},
		{"not zlib", []byte{0xff, 0xee}, objInvalid, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, data, err := readLooseObject(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("readLooseObject() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.wantTyp {
				t.Errorf("readLooseObject() type = %q, want %q", got, tt.wantTyp)
			}
			if tt.wantTyp == objCommit && !bytes.Equal(data, commitData) {
				t.Errorf("readLooseObject() data = %q, want %q", data, commitData)
			}
		})
	}
}

func TestReadOfsOffset(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want int64
	}{
		{"zero", []byte{0x00}, 0},
		{"one", []byte{0x01}, 1},
		{"max single byte", []byte{0x7f}, 127},
		{"two bytes basic", []byte{0x80, 0x00}, 128},
		{"two bytes", []byte{0x80, 0x01}, 129},
		{"two bytes carry", []byte{0x81, 0x01}, 257},
		{"three bytes", []byte{0xff, 0xff, 0x7f}, 2113663},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readOfsOffset(bufio.NewReader(bytes.NewReader(tt.in)))
			if err != nil {
				t.Fatalf("readOfsOffset() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("readOfsOffset() = %d, want %d", got, tt.want)
			}
		})
	}
}

func buildIdx(names []string) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xff, 't', 'O', 'c'})
	_ = binary.Write(&buf, binary.BigEndian, uint32(2))

	first := make([]byte, len(names))
	for i, n := range names {
		b, _ := hex.DecodeString(n)
		first[i] = b[0]
	}
	for i := 0; i < 256; i++ {
		var c uint32
		for _, b := range first {
			if int(b) <= i {
				c++
			}
		}
		_ = binary.Write(&buf, binary.BigEndian, c)
	}
	for _, n := range names {
		b, _ := hex.DecodeString(n)
		buf.Write(b)
	}
	return buf.Bytes()
}

func buildPack(objects [][2]any) []byte {
	var buf bytes.Buffer
	buf.WriteString("PACK")
	_ = binary.Write(&buf, binary.BigEndian, uint32(2))
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(objects)))
	for _, o := range objects {
		typ := o[0].(byte)
		data := o[1].([]byte)
		size := len(data)
		buf.WriteByte((typ << 4) | byte(size&0x0f))
		buf.Write(zlibBytes(data))
	}
	return buf.Bytes()
}

func buildIndexEntry(sha string, name string) []byte {
	var buf bytes.Buffer
	buf.Write(make([]byte, 40))
	b, _ := hex.DecodeString(sha)
	buf.Write(b)
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(name)))
	buf.WriteString(name)
	entryLen := 62 + len(name)
	padding := (8 - (entryLen % 8)) % 8
	buf.Write(make([]byte, padding))
	return buf.Bytes()
}

func TestRefsFromIdx(t *testing.T) {
	names := []string{shaA, shaB, shaC}
	tests := []struct {
		name    string
		in      func() []byte
		want    []string
		wantErr bool
	}{
		{"valid", func() []byte { return buildIdx(names) }, names, false},
		{"bad magic", func() []byte { return append([]byte{0xde, 0xad, 0xbe, 0xef}, buildIdx(names)[4:]...) }, nil, true},
		{"bad version", func() []byte {
			b := buildIdx(names)
			b[4] = 0
			b[5] = 0
			b[6] = 0
			b[7] = 3
			return b
		}, nil, true},
		{"too short", func() []byte { return buildIdx(names)[:40] }, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pack-"+shaA+".idx")
			if err := os.WriteFile(path, tt.in(), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := refsFromIdx(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("refsFromIdx() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !equalStrings(got, tt.want) {
				t.Errorf("refsFromIdx() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRefsFromPack(t *testing.T) {
	commitData := []byte("tree " + shaC + "\nparent " + shaA + "\n\nauthor A <a@b> 1 +0000\n")
	treeData := append([]byte("100644 a.txt\x00"), bytes.Repeat([]byte{0x11}, 20)...)

	dir := t.TempDir()
	packPath := filepath.Join(dir, "pack-"+shaA+".pack")
	idxPath := filepath.Join(dir, "pack-"+shaA+".idx")
	pack := buildPack([][2]any{
		{packCommit, commitData},
		{packTree, treeData},
	})
	if err := os.WriteFile(packPath, pack, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(idxPath, buildIdx([]string{shaA, shaB}), 0o644); err != nil {
		t.Fatal(err)
	}

	packed, all, err := refsFromPack(packPath, idxPath)
	if err != nil {
		t.Fatalf("refsFromPack() error = %v", err)
	}
	if want := []string{shaA, shaB}; !equalStrings(packed, want) {
		t.Errorf("packed = %v, want %v", packed, want)
	}
	if want := []string{shaC, shaA, shaA}; !equalStrings(all, want) {
		t.Errorf("all = %v, want %v", all, want)
	}
}

func TestRefsFromIndex(t *testing.T) {
	tests := []struct {
		name    string
		in      func() []byte
		want    []string
		wantErr bool
	}{
		{"valid", func() []byte {
			var buf bytes.Buffer
			buf.WriteString("DIRC")
			_ = binary.Write(&buf, binary.BigEndian, uint32(2))
			_ = binary.Write(&buf, binary.BigEndian, uint32(1))
			buf.Write(buildIndexEntry("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "hello"))
			return buf.Bytes()
		}, []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, false},
		{"too short", func() []byte { return []byte("DIRC") }, nil, true},
		{"bad signature", func() []byte { return []byte("XXXX1234") }, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "index")
			if err := os.WriteFile(path, tt.in(), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := refsFromIndex(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("refsFromIndex() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !equalStrings(got, tt.want) {
				t.Errorf("refsFromIndex() = %v, want %v", got, tt.want)
			}
		})
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
