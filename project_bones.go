package spineparser

import (
	"bytes"
	"fmt"
)

var projectBoneTablePrefix = []byte{0x0f, 0x01}

// ProjectBoneRecord identifies one bone object in project serialization order.
// ParentReference is the Kryo reference used by the bone's parent field.
type ProjectBoneRecord struct {
	Name            string `json:"name"`
	Offset          int    `json:"offset"`
	ParentReference int    `json:"parentReference"`
	NameEncoding    string `json:"nameEncoding"`
}

// ProjectBoneDirectory contains directly decoded modern project bone names.
type ProjectBoneDirectory struct {
	Format       string              `json:"format"`
	HeaderOffset int                 `json:"headerOffset"`
	ClassID      int                 `json:"classId"`
	Count        int                 `json:"count"`
	Records      []ProjectBoneRecord `json:"records"`
}

// DiscoverProjectBones decodes bone names, object offsets, and parent
// references from a modern Spine Pro project without launching Spine Editor.
func DiscoverProjectBones(payload []byte) (*ProjectBoneDirectory, error) {
	if len(payload) == 0 {
		return nil, &ParseError{Code: ErrInvalidInput, Msg: "project payload is empty"}
	}
	candidates := make([]ProjectBoneDirectory, 0, 1)
	for headerOffset := 0; headerOffset+2 < len(payload); headerOffset++ {
		if !bytes.HasPrefix(payload[headerOffset:], projectBoneTablePrefix) {
			continue
		}
		count, cursor, ok := readPositiveVarint(payload, headerOffset+2)
		if !ok || count < 1 || count > 100_000 ||
			cursor+3 >= len(payload) ||
			payload[cursor] != 0x0c ||
			payload[cursor+1] != 0x01 {
			continue
		}
		classID := int(payload[cursor+2])
		firstRecord := cursor + 1
		prefix := []byte{0x01, byte(classID), 0x00, 0x03, 0x01}
		if !bytes.HasPrefix(payload[firstRecord:], prefix) {
			continue
		}
		records := scanProjectBoneRecords(
			payload,
			firstRecord,
			classID,
			count,
		)
		if len(records) != count || records[0].ParentReference != 0 ||
			!uniqueProjectBoneNames(records) {
			continue
		}
		candidates = append(candidates, ProjectBoneDirectory{
			Format:       "kryo-bone-table-v1",
			HeaderOffset: headerOffset,
			ClassID:      classID,
			Count:        count,
			Records:      records,
		})
	}
	if len(candidates) == 0 {
		return nil, &ParseError{
			Code: ErrInvalidProject,
			Msg:  "supported project bone table was not found",
		}
	}
	if len(candidates) != 1 {
		return nil, &ParseError{
			Code: ErrInvalidProject,
			Msg:  fmt.Sprintf("project contains %d bone table candidates", len(candidates)),
		}
	}
	return &candidates[0], nil
}

func scanProjectBoneRecords(
	payload []byte,
	firstOffset int,
	classID int,
	count int,
) []ProjectBoneRecord {
	prefix := []byte{0x01, byte(classID), 0x00, 0x03, 0x01}
	records := make([]ProjectBoneRecord, 0, count)
	for offset := firstOffset; offset < len(payload) && len(records) < count; {
		relative := bytes.Index(payload[offset:], prefix)
		if relative < 0 {
			break
		}
		recordOffset := offset + relative
		tokenOffset := recordOffset + len(prefix)
		name, afterName, encoding, ok := decodeProjectBoneName(
			payload,
			firstOffset,
			recordOffset,
			tokenOffset,
		)
		if !ok || afterName+3 > len(payload) ||
			payload[afterName] != 0x02 ||
			(payload[afterName+1] != 0x00 && payload[afterName+1] != 0x01) ||
			payload[afterName+2] != 0x03 {
			offset = recordOffset + 1
			continue
		}
		parentReference, _, ok := readPositiveVarint(payload, afterName+3)
		if !ok || (len(records) > 0 && parentReference < 1) {
			offset = recordOffset + 1
			continue
		}
		records = append(records, ProjectBoneRecord{
			Name:            name,
			Offset:          recordOffset,
			ParentReference: parentReference,
			NameEncoding:    encoding,
		})
		offset = afterName + 1
	}
	return records
}

func decodeProjectBoneName(
	payload []byte,
	firstOffset int,
	recordOffset int,
	tokenOffset int,
) (string, int, string, bool) {
	if tokenOffset >= len(payload) {
		return "", tokenOffset, "", false
	}
	if payload[tokenOffset] == 0x01 {
		name, afterName, ok := decodeProjectASCII(payload, tokenOffset+1)
		return name, afterName, "inline", ok
	}
	_, afterReference, ok := readPositiveVarint(payload, tokenOffset)
	if !ok || recordOffset < 1 || payload[recordOffset-1] != 0x02 {
		return "", tokenOffset, "", false
	}
	searchStart := recordOffset - 512
	if searchStart < firstOffset {
		searchStart = firstOffset
	}
	for classID := 0; classID < 64; classID++ {
		wrapperPrefix := []byte{
			0x01, byte(classID), 0x00, 0x03, 0x01, 0x01,
		}
		wrapperOffset := bytes.LastIndex(
			payload[searchStart:recordOffset],
			wrapperPrefix,
		)
		if wrapperOffset < 0 {
			continue
		}
		wrapperOffset += searchStart
		name, end, decoded := decodeProjectASCII(
			payload,
			wrapperOffset+len(wrapperPrefix),
		)
		if decoded && end == recordOffset-1 {
			return name, afterReference, "wrapper-reference", true
		}
	}
	return "", tokenOffset, "", false
}

func uniqueProjectBoneNames(records []ProjectBoneRecord) bool {
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record.Name == "" {
			return false
		}
		if _, exists := seen[record.Name]; exists {
			return false
		}
		seen[record.Name] = struct{}{}
	}
	return true
}
