package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// maxObjectBuffer bounds how much of a single decompressed object is
// buffered for ref extraction. Larger objects (e.g. giant blobs) are
// streamed and discarded instead of loaded into memory.
const maxObjectBuffer = 64 << 20

type objType string

const (
	objInvalid objType = ""
	objCommit  objType = "commit"
	objTree    objType = "tree"
	objBlob    objType = "blob"
	objTag     objType = "tag"
)

const (
	packCommit   byte = 1
	packTree     byte = 2
	packBlob     byte = 3
	packTag      byte = 4
	packOfsDelta byte = 6
	packRefDelta byte = 7
)

func getReferencedSHA1(objPath string) ([]string, error) {
	f, err := os.Open(objPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	t, data, err := readLooseObject(f)
	if err != nil {
		return nil, err
	}
	return referencedSHAsFromObject(t, data)
}

// readLooseObject streams a loose object from r instead of buffering the
// whole file. Blob and tag content is discarded after the header; commit
// and tree content is buffered only up to maxObjectBuffer.
func readLooseObject(r io.Reader) (objType, []byte, error) {
	zr, err := zlib.NewReader(r)
	if err != nil {
		return objInvalid, nil, err
	}
	defer zr.Close()

	hdr, err := readObjectHeader(zr)
	if err != nil {
		return objInvalid, nil, err
	}
	fields := strings.SplitN(hdr, " ", 2)
	if len(fields) != 2 {
		return objInvalid, nil, fmt.Errorf("invalid object header")
	}

	t := parseObjType(fields[0])
	if t == objInvalid {
		return objInvalid, nil, fmt.Errorf("unknown object type %q", fields[0])
	}
	size, err := strconv.Atoi(fields[1])
	if err != nil || size < 0 {
		return objInvalid, nil, fmt.Errorf("invalid object size %q", fields[1])
	}

	if (t != objCommit && t != objTree) || int64(size) > maxObjectBuffer {
		_, _ = io.CopyN(io.Discard, zr, int64(size))
		return t, nil, nil
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(zr, data); err != nil {
		return objInvalid, nil, fmt.Errorf("reading object content: %w", err)
	}
	return t, data, nil
}

// readObjectHeader reads the "<type> <size>\0" prefix of a decompressed
// loose object, bounded to avoid unbounded reads on malformed objects.
func readObjectHeader(zr io.Reader) (string, error) {
	const maxHeaderLen = 128
	var hdr []byte
	one := make([]byte, 1)
	for {
		n, err := zr.Read(one)
		if err != nil {
			if err == io.EOF {
				return "", fmt.Errorf("invalid object")
			}
			return "", err
		}
		if n == 0 {
			continue
		}
		if one[0] == 0 {
			return string(hdr), nil
		}
		hdr = append(hdr, one[0])
		if len(hdr) > maxHeaderLen {
			return "", fmt.Errorf("invalid object header")
		}
	}
}

func parseObjType(s string) objType {
	switch objType(s) {
	case objCommit, objTree, objBlob, objTag:
		return objType(s)
	}
	return objInvalid
}

func referencedSHAsFromObject(t objType, data []byte) ([]string, error) {
	switch t {
	case objCommit:
		return refsFromCommit(data)
	case objTree:
		return refsFromTree(data), nil
	default:
		return nil, nil
	}
}

func refsFromCommit(data []byte) ([]string, error) {
	var refs []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "tree", "parent":
			if len(fields[1]) == 40 {
				refs = append(refs, fields[1])
			}
		}
	}
	return refs, scanner.Err()
}

func refsFromTree(data []byte) []string {
	var refs []string
	for len(data) > 0 {
		sp := bytes.IndexByte(data, ' ')
		if sp < 0 {
			break
		}
		data = data[sp+1:]
		nul := bytes.IndexByte(data, 0)
		if nul < 0 || len(data) < nul+1+20 {
			break
		}
		refs = append(refs, hex.EncodeToString(data[nul+1:nul+1+20]))
		data = data[nul+1+20:]
	}
	return refs
}

func refsFromPack(packPath, idxPath string) (packed, all []string, err error) {
	packed, err = refsFromIdx(idxPath)
	if err != nil {
		return nil, nil, err
	}

	all, err = refsFromPackObjects(packPath)
	if err != nil {
		return nil, nil, err
	}
	return packed, all, nil
}

func refsFromIdx(idxPath string) ([]string, error) {
	f, err := os.Open(idxPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Read only the header/fanout and the object-name table; the CRC and
	// offset tables that follow are not needed for ref extraction.
	const fanoutLen = 4 + 4 + 256*4
	hdr := make([]byte, fanoutLen)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return nil, fmt.Errorf("invalid idx file")
	}
	if !bytes.Equal(hdr[:4], []byte{0xff, 't', 'O', 'c'}) {
		return nil, fmt.Errorf("invalid idx file")
	}
	if version := binary.BigEndian.Uint32(hdr[4:8]); version != 2 {
		return nil, fmt.Errorf("unsupported idx version %d", version)
	}

	count := binary.BigEndian.Uint32(hdr[4+256*4 : fanoutLen])
	names := make([]byte, int64(count)*20)
	if _, err := io.ReadFull(f, names); err != nil {
		return nil, fmt.Errorf("idx too short")
	}

	refs := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		refs = append(refs, hex.EncodeToString(names[i*20:(i+1)*20]))
	}
	return refs, nil
}

func refsFromPackObjects(packPath string) ([]string, error) {
	f, err := os.Open(packPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	br := bufio.NewReader(f)
	hdr := make([]byte, 12)
	if _, err := io.ReadFull(br, hdr); err != nil {
		return nil, err
	}
	if string(hdr[:4]) != "PACK" {
		return nil, fmt.Errorf("invalid pack signature")
	}
	count := binary.BigEndian.Uint32(hdr[8:12])

	var refs []string
	for i := uint32(0); i < count; i++ {
		typ, data, err := readPackObject(br)
		if err != nil {
			return nil, err
		}
		switch typ {
		case packCommit:
			commitRefs, err := refsFromCommit(data)
			if err != nil {
				return nil, err
			}
			refs = append(refs, commitRefs...)
		case packTree:
			refs = append(refs, refsFromTree(data)...)
		}
	}
	return refs, nil
}

func readPackObject(br *bufio.Reader) (byte, []byte, error) {
	c, err := br.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	typ := (c >> 4) & 0x7
	size := int64(c & 0x0f)
	for shift := uint(4); c&0x80 != 0; shift += 7 {
		c, err = br.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		size |= int64(c&0x7f) << shift
	}

	switch typ {
	case packOfsDelta:
		if _, err := readOfsOffset(br); err != nil {
			return 0, nil, err
		}
	case packRefDelta:
		base := make([]byte, 20)
		if _, err := io.ReadFull(br, base); err != nil {
			return 0, nil, err
		}
	}

	zr, err := zlib.NewReader(br)
	if err != nil {
		return 0, nil, err
	}

	var data []byte
	if (typ == packCommit || typ == packTree) && size <= maxObjectBuffer {
		data = make([]byte, size)
		if _, err = io.ReadFull(zr, data); err != nil {
			closeErr := zr.Close()
			if closeErr != nil {
				return 0, nil, closeErr
			}
			return 0, nil, err
		}
	} else {
		_, err = io.CopyN(io.Discard, zr, size)
	}
	closeErr := zr.Close()
	if err != nil {
		return 0, nil, err
	}
	if closeErr != nil {
		return 0, nil, closeErr
	}
	return typ, data, nil
}

// readOfsOffset decodes the off-by-one variable-length offset used by
// OFS_DELTA objects, matching git's unpack-objects encoding.
func readOfsOffset(br *bufio.Reader) (int64, error) {
	c, err := br.ReadByte()
	if err != nil {
		return 0, err
	}
	off := int64(c & 0x7f)
	for c&0x80 != 0 {
		off++
		c, err = br.ReadByte()
		if err != nil {
			return 0, err
		}
		off = (off << 7) + int64(c&0x7f)
	}
	return off, nil
}

func refsFromIndex(indexPath string) ([]string, error) {
	f, err := os.Open(indexPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	head := make([]byte, 12)
	if _, err := io.ReadFull(f, head); err != nil {
		return nil, fmt.Errorf("index too short")
	}
	if string(head[:4]) != "DIRC" {
		return nil, fmt.Errorf("invalid index signature")
	}

	count := binary.BigEndian.Uint32(head[8:12])

	var refs []string
	for i := uint32(0); i < count; i++ {
		var entry [62]byte
		if _, err := io.ReadFull(f, entry[:]); err != nil {
			break
		}
		refs = append(refs, hex.EncodeToString(entry[40:60]))
		nameLen := binary.BigEndian.Uint16(entry[60:62])
		entryLen := 62 + int(nameLen&0xfff)
		if nameLen&0x8000 != 0 {
			var ext [4]byte
			if _, err := io.ReadFull(f, ext[:]); err != nil {
				break
			}
			entryLen = 62 + int(binary.BigEndian.Uint32(ext[:]))
		}
		if _, err := io.CopyN(io.Discard, f, int64(entryLen-62)); err != nil {
			break
		}
		padding := (8 - (entryLen % 8)) % 8
		if _, err := io.CopyN(io.Discard, f, int64(padding)); err != nil {
			break
		}
	}
	return refs, nil
}

func collectObjectSHAs(directory string) (objs map[string]struct{}, packedObjs map[string]struct{}, err error) {
	objs = make(map[string]struct{})
	packedObjs = make(map[string]struct{})

	shaRe := regexp.MustCompile(`(^|\s)([a-f0-9]{40})($|\s)`)

	files := []string{
		filepath.Join(directory, ".git", "packed-refs"),
		filepath.Join(directory, ".git", "info", "refs"),
		filepath.Join(directory, ".git", "FETCH_HEAD"),
		filepath.Join(directory, ".git", "ORIG_HEAD"),
	}

	for _, sub := range []string{"refs", "logs"} {
		root := filepath.Join(directory, ".git", sub)
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				files = append(files, path)
			}
			return nil
		})
	}

	for _, fp := range files {
		content, err := os.ReadFile(fp)
		if err != nil {
			continue
		}
		for _, m := range shaRe.FindAllStringSubmatch(string(content), -1) {
			objs[m[2]] = struct{}{}
		}
	}

	indexPath := filepath.Join(directory, ".git", "index")
	if indexRefs, err := refsFromIndex(indexPath); err == nil {
		for _, ref := range indexRefs {
			objs[ref] = struct{}{}
		}
	}

	packDir := filepath.Join(directory, ".git", "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, "pack-") || !strings.HasSuffix(name, ".pack") {
				continue
			}
			packPath := filepath.Join(packDir, name)
			idxPath := filepath.Join(packDir, strings.TrimSuffix(name, ".pack")+".idx")
			packed, refs, err := refsFromPack(packPath, idxPath)
			if err != nil {
				continue
			}
			for _, p := range packed {
				packedObjs[p] = struct{}{}
			}
			for _, ref := range refs {
				objs[ref] = struct{}{}
			}
		}
	}

	return objs, packedObjs, nil
}
