package spineparser

import (
	"bytes"
	"fmt"
	"strings"
)

// ProjectAnimationDeleteReport describes one fail-closed deletion from the
// terminal entry of the modern project animation map.
type ProjectAnimationDeleteReport struct {
	Animation     string `json:"animation"`
	HeaderOffset  int    `json:"headerOffset"`
	CountOffset   int    `json:"countOffset"`
	PreviousCount int    `json:"previousCount"`
	Count         int    `json:"count"`
	RegionStart   int    `json:"regionStart"`
	RegionEnd     int    `json:"regionEnd"`
	BytesRemoved  int    `json:"bytesRemoved"`
}

// DeleteLastProjectAnimation clones a modern project and removes the final
// animation map entry. Restricting deletion to the terminal entry is important:
// Kryo assigns reference IDs implicitly, so removing an earlier object would
// require decoding and renumbering every later object reference.
//
// The expected animation name must identify the final map entry, so stale
// agent plans fail closed. Duplicate leaf names are allowed because this API
// selects by terminal position, not by name lookup. Deleting the only animation
// leaves a valid terminal empty map. The input document is never mutated.
func DeleteLastProjectAnimation(
	document *ProjectDocument,
	expectedAnimation string,
) (*ProjectDocument, ProjectAnimationDeleteReport, error) {
	if document == nil || len(document.Payload) == 0 {
		return nil, ProjectAnimationDeleteReport{},
			&ParseError{Code: ErrInvalidInput, Msg: "project payload is empty"}
	}
	if strings.TrimSpace(expectedAnimation) == "" {
		return nil, ProjectAnimationDeleteReport{},
			&ParseError{Code: ErrInvalidInput, Msg: "animation is required"}
	}
	directory, err := DiscoverProjectAnimations(document.Payload)
	if err != nil {
		return nil, ProjectAnimationDeleteReport{}, err
	}
	if directory.Count == 0 || len(directory.Records) == 0 {
		return nil, ProjectAnimationDeleteReport{},
			&ParseError{
				Code: ErrInvalidProject,
				Msg:  "project animation map is empty",
			}
	}
	last := directory.Records[len(directory.Records)-1]
	if last.Name != expectedAnimation {
		return nil, ProjectAnimationDeleteReport{}, fmt.Errorf(
			"animation %q is not the final project animation; final animation is %q",
			expectedAnimation,
			last.Name,
		)
	}
	if last.EndOffset != len(document.Payload) {
		return nil, ProjectAnimationDeleteReport{},
			&ParseError{
				Code: ErrInvalidProject,
				Msg:  "final animation record is not terminal",
			}
	}

	countOffset := directory.HeaderOffset + len(modernAnimationHeaderPrefix)
	count, countEnd, ok := readPositiveVarint(document.Payload, countOffset)
	if !ok || count != directory.Count || countEnd > last.Offset {
		return nil, ProjectAnimationDeleteReport{},
			&ParseError{
				Code: ErrInvalidProject,
				Msg:  "animation map count does not match its directory",
			}
	}
	newCountBytes := appendPositiveVarint(nil, directory.Count-1)
	payload := make(
		[]byte,
		0,
		len(document.Payload)-(last.EndOffset-last.Offset)+
			len(newCountBytes)-(countEnd-countOffset),
	)
	payload = append(payload, document.Payload[:countOffset]...)
	payload = append(payload, newCountBytes...)
	payload = append(payload, document.Payload[countEnd:last.Offset]...)

	verified, err := DiscoverProjectAnimations(payload)
	if err != nil {
		return nil, ProjectAnimationDeleteReport{},
			fmt.Errorf("verify deleted project animation: %w", err)
	}
	if verified.Count != directory.Count-1 ||
		len(verified.Records) != len(directory.Records)-1 {
		return nil, ProjectAnimationDeleteReport{},
			&ParseError{
				Code: ErrInvalidProject,
				Msg:  "deleted animation map failed count verification",
			}
	}
	countDelta := len(newCountBytes) - (countEnd - countOffset)
	for index, record := range verified.Records {
		previous := directory.Records[index]
		if record.Name != previous.Name ||
			record.Offset != previous.Offset+countDelta {
			return nil, ProjectAnimationDeleteReport{},
				&ParseError{
					Code: ErrInvalidProject,
					Msg:  "deleted animation map changed a retained record",
				}
		}
		previousEnd := previous.EndOffset
		if index == len(verified.Records)-1 {
			previousEnd = last.Offset
		}
		if !bytes.Equal(
			payload[record.Offset:record.EndOffset],
			document.Payload[previous.Offset:previousEnd],
		) {
			return nil, ProjectAnimationDeleteReport{},
				&ParseError{
					Code: ErrInvalidProject,
					Msg:  "deleted animation map changed retained record bytes",
				}
		}
	}

	report := ProjectAnimationDeleteReport{
		Animation:     expectedAnimation,
		HeaderOffset:  directory.HeaderOffset,
		CountOffset:   countOffset,
		PreviousCount: directory.Count,
		Count:         directory.Count - 1,
		RegionStart:   last.Offset,
		RegionEnd:     last.EndOffset,
		BytesRemoved:  len(document.Payload) - len(payload),
	}
	return &ProjectDocument{
		Inspection: document.Inspection,
		Payload:    payload,
	}, report, nil
}

func appendPositiveVarint(output []byte, value int) []byte {
	for {
		current := byte(value & 0x7f)
		value >>= 7
		if value == 0 {
			return append(output, current)
		}
		output = append(output, current|0x80)
	}
}
