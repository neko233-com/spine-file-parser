package spineparser

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

const projectTimelineEvent = 12

// ProjectEventKey is one event key whose event payload remains byte-preserved.
// Frame is the editor frame number; Time is Frame / 30.
type ProjectEventKey struct {
	Index       int     `json:"index"`
	Frame       float32 `json:"frame"`
	Time        float32 `json:"time"`
	Offset      int     `json:"offset"`
	FrameOffset int     `json:"frameOffset"`
}

// ProjectEventTimeline identifies one event timeline without guessing event
// definition references or optional event values.
type ProjectEventTimeline struct {
	TimelineReference int               `json:"timelineReference"`
	KeyReference      int               `json:"keyReference"`
	Offset            int               `json:"offset"`
	Keys              []ProjectEventKey `json:"keys"`
}

// ProjectEventTimelineDirectory contains event timelines from one top-level
// animation record.
type ProjectEventTimelineDirectory struct {
	Animation   string                 `json:"animation"`
	RegionStart int                    `json:"regionStart"`
	RegionEnd   int                    `json:"regionEnd"`
	FrameRate   int                    `json:"frameRate"`
	Timelines   []ProjectEventTimeline `json:"timelines"`
}

// DiscoverProjectEventTimelines decodes fixed-topology event key frames while
// preserving event definitions and values as opaque bytes.
func DiscoverProjectEventTimelines(
	payload []byte,
	animation string,
) (*ProjectEventTimelineDirectory, error) {
	record, err := uniqueProjectAnimationRecord(payload, animation)
	if err != nil {
		return nil, err
	}
	timelines := discoverProjectEventTimelinesInRecord(
		payload,
		record.Offset,
		record.EndOffset,
	)
	if len(timelines) == 0 {
		return nil, &ParseError{
			Code: ErrInvalidProject,
			Msg: fmt.Sprintf(
				"animation %q contains no supported event timelines",
				animation,
			),
		}
	}
	return &ProjectEventTimelineDirectory{
		Animation:   animation,
		RegionStart: record.Offset,
		RegionEnd:   record.EndOffset,
		FrameRate:   projectAnimationFrameRate,
		Timelines:   timelines,
	}, nil
}

// ProjectEventFrameEdit retimes one existing event key.
type ProjectEventFrameEdit struct {
	TimelineReference int     `json:"timelineReference"`
	KeyReference      int     `json:"keyReference"`
	TimelineOffset    int     `json:"timelineOffset"`
	KeyIndex          int     `json:"keyIndex"`
	From              float32 `json:"from"`
	To                float32 `json:"to"`
}

// ProjectEventPatch controls event-key retiming and optional animation rename.
type ProjectEventPatch struct {
	Animation       string                  `json:"animation"`
	TargetAnimation string                  `json:"targetAnimation,omitempty"`
	Edits           []ProjectEventFrameEdit `json:"edits"`
}

// ProjectEventFrameChange reports one exact event-key frame edit.
type ProjectEventFrameChange struct {
	TimelineReference int     `json:"timelineReference"`
	KeyReference      int     `json:"keyReference"`
	TimelineOffset    int     `json:"timelineOffset"`
	KeyIndex          int     `json:"keyIndex"`
	From              float32 `json:"from"`
	To                float32 `json:"to"`
	Offset            int     `json:"offset"`
}

// ProjectEventPatchReport is safe to inspect before serialization.
type ProjectEventPatchReport struct {
	Animation       string                    `json:"animation"`
	TargetAnimation string                    `json:"targetAnimation,omitempty"`
	RegionStart     int                       `json:"regionStart"`
	RegionEnd       int                       `json:"regionEnd"`
	Changes         []ProjectEventFrameChange `json:"changes"`
}

// PatchProjectEventFrames clones a project and retimes explicitly selected
// event keys. Event objects, values, curves, and topology remain unchanged.
func PatchProjectEventFrames(
	document *ProjectDocument,
	patch ProjectEventPatch,
) (*ProjectDocument, ProjectEventPatchReport, error) {
	if document == nil || len(document.Payload) == 0 {
		return nil, ProjectEventPatchReport{},
			&ParseError{Code: ErrInvalidInput, Msg: "project payload is empty"}
	}
	if len(patch.Edits) == 0 {
		return nil, ProjectEventPatchReport{},
			&ParseError{Code: ErrInvalidInput, Msg: "at least one event edit is required"}
	}
	directory, err := DiscoverProjectEventTimelines(
		document.Payload,
		patch.Animation,
	)
	if err != nil {
		return nil, ProjectEventPatchReport{}, err
	}
	byOffset := make(map[int][]ProjectEventTimeline)
	for _, timeline := range directory.Timelines {
		byOffset[timeline.Offset] = append(byOffset[timeline.Offset], timeline)
	}
	payload := append([]byte(nil), document.Payload...)
	report := ProjectEventPatchReport{
		Animation:       patch.Animation,
		TargetAnimation: patch.TargetAnimation,
		RegionStart:     directory.RegionStart,
		RegionEnd:       directory.RegionEnd,
		Changes:         make([]ProjectEventFrameChange, 0, len(patch.Edits)),
	}
	seen := make(map[[2]int]struct{}, len(patch.Edits))
	for editIndex, edit := range patch.Edits {
		if !finiteProjectFloat(edit.From) || !finiteProjectFloat(edit.To) ||
			edit.To < 0 {
			return nil, ProjectEventPatchReport{}, fmt.Errorf(
				"edit %d: event frames must be finite and target non-negative",
				editIndex,
			)
		}
		if math.Float32bits(edit.From) == math.Float32bits(edit.To) {
			return nil, ProjectEventPatchReport{},
				fmt.Errorf("edit %d: from and to must differ", editIndex)
		}
		selection := [2]int{edit.TimelineOffset, edit.KeyIndex}
		if _, duplicate := seen[selection]; duplicate {
			return nil, ProjectEventPatchReport{},
				fmt.Errorf("edit %d: duplicate timelineOffset/keyIndex", editIndex)
		}
		seen[selection] = struct{}{}
		matches := byOffset[edit.TimelineOffset]
		if len(matches) != 1 {
			return nil, ProjectEventPatchReport{}, fmt.Errorf(
				"edit %d: timelineOffset %d matched %d event timelines",
				editIndex,
				edit.TimelineOffset,
				len(matches),
			)
		}
		timeline := matches[0]
		if timeline.TimelineReference != edit.TimelineReference ||
			timeline.KeyReference != edit.KeyReference {
			return nil, ProjectEventPatchReport{}, fmt.Errorf(
				"edit %d: timeline identity is timelineReference %d keyReference %d, expected %d/%d",
				editIndex,
				timeline.TimelineReference,
				timeline.KeyReference,
				edit.TimelineReference,
				edit.KeyReference,
			)
		}
		if edit.KeyIndex < 0 || edit.KeyIndex >= len(timeline.Keys) {
			return nil, ProjectEventPatchReport{}, fmt.Errorf(
				"edit %d: keyIndex %d is outside [0,%d)",
				editIndex,
				edit.KeyIndex,
				len(timeline.Keys),
			)
		}
		key := timeline.Keys[edit.KeyIndex]
		if math.Float32bits(key.Frame) != math.Float32bits(edit.From) {
			return nil, ProjectEventPatchReport{}, fmt.Errorf(
				"edit %d: key frame is %v, expected %v",
				editIndex,
				key.Frame,
				edit.From,
			)
		}
		binary.BigEndian.PutUint32(
			payload[key.FrameOffset:key.FrameOffset+4],
			math.Float32bits(edit.To),
		)
		report.Changes = append(report.Changes, ProjectEventFrameChange{
			TimelineReference: edit.TimelineReference,
			KeyReference:      edit.KeyReference,
			TimelineOffset:    edit.TimelineOffset,
			KeyIndex:          edit.KeyIndex,
			From:              edit.From,
			To:                edit.To,
			Offset:            key.FrameOffset,
		})
	}
	if err := validateProjectEventFrameOrder(payload, directory); err != nil {
		return nil, ProjectEventPatchReport{}, err
	}
	if strings.TrimSpace(patch.TargetAnimation) != "" &&
		patch.TargetAnimation != patch.Animation {
		payload, err = renameProjectAnimationRecord(
			payload,
			directory.RegionStart,
			patch.Animation,
			patch.TargetAnimation,
		)
		if err != nil {
			return nil, ProjectEventPatchReport{}, err
		}
		sourceName, _ := encodeProjectString(patch.Animation)
		targetName, _ := encodeProjectString(patch.TargetAnimation)
		delta := len(targetName) - len(sourceName)
		for index := range report.Changes {
			report.Changes[index].Offset += delta
			report.Changes[index].TimelineOffset += delta
		}
		report.RegionEnd += delta
	}
	return &ProjectDocument{
		Inspection: document.Inspection,
		Payload:    payload,
	}, report, nil
}

func discoverProjectEventTimelinesInRecord(
	payload []byte,
	start int,
	end int,
) []ProjectEventTimeline {
	timelines := make([]ProjectEventTimeline, 0, 1)
	for offset := start; offset+len(projectTimelinePrefix) < end; offset++ {
		if !bytes.HasPrefix(payload[offset:end], projectTimelinePrefix) {
			continue
		}
		timelineReference, cursor, ok := readPositiveVarint(
			payload,
			offset+len(projectTimelinePrefix),
		)
		if !ok || timelineReference < projectFirstWireReference ||
			cursor+2 >= end ||
			payload[cursor] != projectTimelineEvent ||
			payload[cursor+1] != 0x01 {
			continue
		}
		keyCount, keyCursor, ok := readPositiveVarint(payload, cursor+2)
		if !ok || keyCount < 1 || keyCount > 100_000 {
			continue
		}
		keys, keyReference, _, ok := readProjectEventKeys(
			payload,
			keyCursor,
			end,
			timelineReference,
			keyCount,
		)
		if !ok {
			continue
		}
		timelines = append(timelines, ProjectEventTimeline{
			TimelineReference: timelineReference,
			KeyReference:      keyReference,
			Offset:            offset,
			Keys:              keys,
		})
	}
	return timelines
}

func readProjectEventKeys(
	payload []byte,
	offset int,
	end int,
	timelineReference int,
	count int,
) ([]ProjectEventKey, int, int, bool) {
	keys := make([]ProjectEventKey, 0, count)
	keyReference := 0
	cursor := offset
	var previous float32
	for index := 0; index < count; index++ {
		keyOffset, currentKeyReference, frameOffset, ok := findProjectEventKey(
			payload,
			cursor,
			end,
			timelineReference,
			keyReference,
		)
		if !ok {
			return nil, 0, offset, false
		}
		if index == 0 {
			keyReference = currentKeyReference
		}
		frame := readProjectFloat32(payload, frameOffset)
		if !finiteProjectFloat(frame) || frame < 0 ||
			(index > 0 && frame <= previous) {
			return nil, 0, offset, false
		}
		keys = append(keys, ProjectEventKey{
			Index:       index,
			Frame:       frame,
			Time:        frame / projectAnimationFrameRate,
			Offset:      keyOffset,
			FrameOffset: frameOffset,
		})
		previous = frame
		cursor = frameOffset + 4
	}
	return keys, keyReference, cursor, true
}

func findProjectEventKey(
	payload []byte,
	start int,
	end int,
	timelineReference int,
	keyReference int,
) (int, int, int, bool) {
	for cursor := start; cursor+len(projectTimelineKeyPrefix) < end; {
		relative := bytes.Index(payload[cursor:end], projectTimelineKeyPrefix)
		if relative < 0 {
			break
		}
		keyOffset := cursor + relative
		currentTimelineReference, next, ok := readPositiveVarint(
			payload,
			keyOffset+len(projectTimelineKeyPrefix),
		)
		if !ok || currentTimelineReference != timelineReference {
			cursor = keyOffset + 1
			continue
		}
		currentKeyReference, frameOffset, ok := readPositiveVarint(payload, next)
		if !ok || currentKeyReference < projectFirstWireReference ||
			frameOffset+4 > end ||
			(keyReference != 0 && currentKeyReference != keyReference) {
			cursor = keyOffset + 1
			continue
		}
		return keyOffset, currentKeyReference, frameOffset, true
	}
	return 0, 0, 0, false
}

func validateProjectEventFrameOrder(
	payload []byte,
	directory *ProjectEventTimelineDirectory,
) error {
	for _, timeline := range directory.Timelines {
		var previous float32
		for index, key := range timeline.Keys {
			frame := readProjectFloat32(payload, key.FrameOffset)
			if !finiteProjectFloat(frame) || frame < 0 {
				return fmt.Errorf(
					"event timelineReference %d key %d has invalid frame %v",
					timeline.TimelineReference,
					index,
					frame,
				)
			}
			if index > 0 && frame <= previous {
				return fmt.Errorf(
					"event timelineReference %d frames are not strictly increasing at key %d: %v <= %v",
					timeline.TimelineReference,
					index,
					frame,
					previous,
				)
			}
			previous = frame
		}
	}
	return nil
}
