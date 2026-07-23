package spineparser

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

const projectAnimationFrameRate = 30

var (
	projectBoneTimelineGroupPrefix = []byte{0x13, 0x01, 0x05, 0x00}
	projectBoneTimelineMapPrefix   = []byte{0x02, 0x0f, 0x01}
	projectTimelinePrefix          = []byte{0x84, 0x01, 0x01}
	projectTimelineKeyPrefix       = []byte{0x85, 0x01, 0x01}
)

// ProjectRotateKey is one directly decoded rotate key in a modern .spine
// project. Frame is the editor frame number; Time is Frame / 30.
type ProjectRotateKey struct {
	Index       int        `json:"index"`
	Frame       float32    `json:"frame"`
	Time        float32    `json:"time"`
	Value       float32    `json:"value"`
	Offset      int        `json:"offset"`
	ValueOffset int        `json:"valueOffset"`
	Curve       [4]float32 `json:"curve"`
	Flags       [5]byte    `json:"flags"`
}

// ProjectRotateTimeline identifies a rotate timeline by the project's stable
// Kryo bone reference. Bone-name decoding is intentionally not guessed.
type ProjectRotateTimeline struct {
	BoneReference     int                `json:"boneReference"`
	TimelineReference int                `json:"timelineReference"`
	KeyReference      int                `json:"keyReference"`
	Offset            int                `json:"offset"`
	Keys              []ProjectRotateKey `json:"keys"`
}

// ProjectRotateTimelineDirectory contains all rotate timelines in one
// top-level animation record.
type ProjectRotateTimelineDirectory struct {
	Animation   string                  `json:"animation"`
	RegionStart int                     `json:"regionStart"`
	RegionEnd   int                     `json:"regionEnd"`
	FrameRate   int                     `json:"frameRate"`
	Timelines   []ProjectRotateTimeline `json:"timelines"`
}

// DiscoverProjectRotateTimelines decodes modern Spine Pro rotate timelines
// without launching Spine Editor.
func DiscoverProjectRotateTimelines(
	payload []byte,
	animation string,
) (*ProjectRotateTimelineDirectory, error) {
	record, err := uniqueProjectAnimationRecord(payload, animation)
	if err != nil {
		return nil, err
	}
	groups := discoverProjectBoneTimelineGroups(
		payload,
		record.Offset,
		record.EndOffset,
	)
	timelines := make([]ProjectRotateTimeline, 0)
	for index, group := range groups {
		groupEnd := record.EndOffset
		if index+1 < len(groups) {
			groupEnd = groups[index+1].Offset
		}
		timelines = append(
			timelines,
			discoverProjectRotateTimelinesInGroup(
				payload,
				group.Offset,
				groupEnd,
				group.BoneReference,
			)...,
		)
	}
	if len(timelines) == 0 {
		return nil, &ParseError{
			Code: ErrInvalidProject,
			Msg:  fmt.Sprintf("animation %q contains no supported rotate timelines", animation),
		}
	}
	return &ProjectRotateTimelineDirectory{
		Animation:   animation,
		RegionStart: record.Offset,
		RegionEnd:   record.EndOffset,
		FrameRate:   projectAnimationFrameRate,
		Timelines:   timelines,
	}, nil
}

// ProjectRotateValueEdit changes one key selected by bone reference and index.
// From is checked exactly so stale agent plans fail closed.
type ProjectRotateValueEdit struct {
	BoneReference int     `json:"boneReference"`
	KeyIndex      int     `json:"keyIndex"`
	From          float32 `json:"from"`
	To            float32 `json:"to"`
}

// ProjectRotatePatch controls direct semantic rotate-key edits and optional
// animation renaming.
type ProjectRotatePatch struct {
	Animation       string                   `json:"animation"`
	TargetAnimation string                   `json:"targetAnimation,omitempty"`
	Edits           []ProjectRotateValueEdit `json:"edits"`
}

// ProjectRotateValueChange reports one semantic key edit.
type ProjectRotateValueChange struct {
	BoneReference int     `json:"boneReference"`
	KeyIndex      int     `json:"keyIndex"`
	Frame         float32 `json:"frame"`
	From          float32 `json:"from"`
	To            float32 `json:"to"`
	Offset        int     `json:"offset"`
}

// ProjectRotatePatchReport is safe to inspect before serialization.
type ProjectRotatePatchReport struct {
	Animation       string                     `json:"animation"`
	TargetAnimation string                     `json:"targetAnimation,omitempty"`
	RegionStart     int                        `json:"regionStart"`
	RegionEnd       int                        `json:"regionEnd"`
	Changes         []ProjectRotateValueChange `json:"changes"`
}

// PatchProjectRotateValues clones a project and modifies explicitly selected
// rotate keys. It never mutates document.
func PatchProjectRotateValues(
	document *ProjectDocument,
	patch ProjectRotatePatch,
) (*ProjectDocument, ProjectRotatePatchReport, error) {
	if document == nil || len(document.Payload) == 0 {
		return nil, ProjectRotatePatchReport{},
			&ParseError{Code: ErrInvalidInput, Msg: "project payload is empty"}
	}
	if len(patch.Edits) == 0 {
		return nil, ProjectRotatePatchReport{},
			&ParseError{Code: ErrInvalidInput, Msg: "at least one rotate edit is required"}
	}
	directory, err := DiscoverProjectRotateTimelines(document.Payload, patch.Animation)
	if err != nil {
		return nil, ProjectRotatePatchReport{}, err
	}
	timelineByBone := make(map[int][]ProjectRotateTimeline)
	for _, timeline := range directory.Timelines {
		timelineByBone[timeline.BoneReference] = append(
			timelineByBone[timeline.BoneReference],
			timeline,
		)
	}

	payload := append([]byte(nil), document.Payload...)
	report := ProjectRotatePatchReport{
		Animation:       patch.Animation,
		TargetAnimation: patch.TargetAnimation,
		RegionStart:     directory.RegionStart,
		RegionEnd:       directory.RegionEnd,
		Changes:         make([]ProjectRotateValueChange, 0, len(patch.Edits)),
	}
	seen := make(map[[2]int]struct{}, len(patch.Edits))
	for editIndex, edit := range patch.Edits {
		if math.IsNaN(float64(edit.From)) || math.IsInf(float64(edit.From), 0) ||
			math.IsNaN(float64(edit.To)) || math.IsInf(float64(edit.To), 0) {
			return nil, ProjectRotatePatchReport{},
				fmt.Errorf("edit %d: rotate values must be finite", editIndex)
		}
		if math.Float32bits(edit.From) == math.Float32bits(edit.To) {
			return nil, ProjectRotatePatchReport{},
				fmt.Errorf("edit %d: from and to must differ", editIndex)
		}
		key := [2]int{edit.BoneReference, edit.KeyIndex}
		if _, exists := seen[key]; exists {
			return nil, ProjectRotatePatchReport{},
				fmt.Errorf("edit %d: duplicate boneReference/keyIndex", editIndex)
		}
		seen[key] = struct{}{}
		matches := timelineByBone[edit.BoneReference]
		if len(matches) == 0 {
			return nil, ProjectRotatePatchReport{}, fmt.Errorf(
				"edit %d: rotate timeline not found for boneReference %d",
				editIndex,
				edit.BoneReference,
			)
		}
		if len(matches) != 1 {
			return nil, ProjectRotatePatchReport{}, fmt.Errorf(
				"edit %d: boneReference %d matched %d rotate timelines",
				editIndex,
				edit.BoneReference,
				len(matches),
			)
		}
		timeline := matches[0]
		if edit.KeyIndex < 0 || edit.KeyIndex >= len(timeline.Keys) {
			return nil, ProjectRotatePatchReport{}, fmt.Errorf(
				"edit %d: keyIndex %d is outside [0,%d)",
				editIndex,
				edit.KeyIndex,
				len(timeline.Keys),
			)
		}
		selected := timeline.Keys[edit.KeyIndex]
		if math.Float32bits(selected.Value) != math.Float32bits(edit.From) {
			return nil, ProjectRotatePatchReport{}, fmt.Errorf(
				"edit %d: key value is %v, expected %v",
				editIndex,
				selected.Value,
				edit.From,
			)
		}
		binary.BigEndian.PutUint32(
			payload[selected.ValueOffset:selected.ValueOffset+4],
			math.Float32bits(edit.To),
		)
		report.Changes = append(report.Changes, ProjectRotateValueChange{
			BoneReference: edit.BoneReference,
			KeyIndex:      edit.KeyIndex,
			Frame:         selected.Frame,
			From:          edit.From,
			To:            edit.To,
			Offset:        selected.ValueOffset,
		})
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
			return nil, ProjectRotatePatchReport{}, err
		}
		sourceName, _ := encodeProjectString(patch.Animation)
		targetName, _ := encodeProjectString(patch.TargetAnimation)
		delta := len(targetName) - len(sourceName)
		for index := range report.Changes {
			report.Changes[index].Offset += delta
		}
		report.RegionEnd += delta
	}

	return &ProjectDocument{
		Inspection: document.Inspection,
		Payload:    payload,
	}, report, nil
}

type projectBoneTimelineGroup struct {
	Offset        int
	BoneReference int
}

func uniqueProjectAnimationRecord(
	payload []byte,
	animation string,
) (ProjectAnimationRecord, error) {
	directory, err := DiscoverProjectAnimations(payload)
	if err != nil {
		return ProjectAnimationRecord{}, err
	}
	matches := make([]ProjectAnimationRecord, 0, 1)
	for _, record := range directory.Records {
		if record.Name == animation {
			matches = append(matches, record)
		}
	}
	if len(matches) == 0 {
		return ProjectAnimationRecord{}, fmt.Errorf("animation not found: %s", animation)
	}
	if len(matches) != 1 {
		return ProjectAnimationRecord{}, fmt.Errorf(
			"animation name is ambiguous: %s matched %d records",
			animation,
			len(matches),
		)
	}
	return matches[0], nil
}

func discoverProjectBoneTimelineGroups(
	payload []byte,
	start int,
	end int,
) []projectBoneTimelineGroup {
	groups := make([]projectBoneTimelineGroup, 0)
	for offset := start; offset+len(projectBoneTimelineGroupPrefix) < end; offset++ {
		if !bytes.HasPrefix(payload[offset:end], projectBoneTimelineGroupPrefix) {
			continue
		}
		_, cursor, ok := readPositiveVarint(
			payload,
			offset+len(projectBoneTimelineGroupPrefix),
		)
		if !ok || cursor >= end || payload[cursor] != 0x01 {
			continue
		}
		boneReference, cursor, ok := readPositiveVarint(payload, cursor+1)
		if !ok || boneReference < 1 ||
			cursor+len(projectBoneTimelineMapPrefix) > end ||
			!bytes.Equal(
				payload[cursor:cursor+len(projectBoneTimelineMapPrefix)],
				projectBoneTimelineMapPrefix,
			) {
			continue
		}
		groups = append(groups, projectBoneTimelineGroup{
			Offset:        offset,
			BoneReference: boneReference,
		})
		offset = cursor + len(projectBoneTimelineMapPrefix) - 1
	}
	return groups
}

func discoverProjectRotateTimelinesInGroup(
	payload []byte,
	start int,
	end int,
	boneReference int,
) []ProjectRotateTimeline {
	timelines := make([]ProjectRotateTimeline, 0, 1)
	for offset := start; offset+len(projectTimelinePrefix) < end; offset++ {
		if !bytes.HasPrefix(payload[offset:end], projectTimelinePrefix) {
			continue
		}
		timelineReference, cursor, ok := readPositiveVarint(
			payload,
			offset+len(projectTimelinePrefix),
		)
		if !ok || cursor+2 >= end {
			continue
		}
		timelineType := payload[cursor]
		if payload[cursor+1] != 0x01 {
			continue
		}
		keyCount, keyCursor, ok := readPositiveVarint(payload, cursor+2)
		if !ok || timelineType != 0 || keyCount < 1 || keyCount > 100_000 {
			continue
		}
		keys, keyReference, next, ok := readProjectRotateKeys(
			payload,
			keyCursor,
			end,
			timelineReference,
			keyCount,
		)
		if !ok {
			continue
		}
		timelines = append(timelines, ProjectRotateTimeline{
			BoneReference:     boneReference,
			TimelineReference: timelineReference,
			KeyReference:      keyReference,
			Offset:            offset,
			Keys:              keys,
		})
		offset = next - 1
	}
	return timelines
}

func readProjectRotateKeys(
	payload []byte,
	offset int,
	end int,
	timelineReference int,
	count int,
) ([]ProjectRotateKey, int, int, bool) {
	keys := make([]ProjectRotateKey, 0, count)
	keyReference := 0
	cursor := offset
	for index := 0; index < count; index++ {
		keyOffset := cursor
		if cursor+len(projectTimelineKeyPrefix) > end ||
			!bytes.Equal(
				payload[cursor:cursor+len(projectTimelineKeyPrefix)],
				projectTimelineKeyPrefix,
			) {
			return nil, 0, offset, false
		}
		currentTimelineReference, next, ok := readPositiveVarint(
			payload,
			cursor+len(projectTimelineKeyPrefix),
		)
		if !ok || currentTimelineReference != timelineReference {
			return nil, 0, offset, false
		}
		currentKeyReference, next, ok := readPositiveVarint(payload, next)
		if !ok || next+29 > end {
			return nil, 0, offset, false
		}
		if index == 0 {
			keyReference = currentKeyReference
		} else if currentKeyReference != keyReference {
			return nil, 0, offset, false
		}
		frame := math.Float32frombits(binary.BigEndian.Uint32(payload[next:]))
		value := math.Float32frombits(binary.BigEndian.Uint32(payload[next+4:]))
		if math.IsNaN(float64(frame)) || math.IsInf(float64(frame), 0) ||
			frame < 0 ||
			math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, 0, offset, false
		}
		key := ProjectRotateKey{
			Index:       index,
			Frame:       frame,
			Time:        frame / projectAnimationFrameRate,
			Value:       value,
			Offset:      keyOffset,
			ValueOffset: next + 4,
		}
		for curveIndex := range key.Curve {
			curveOffset := next + 8 + curveIndex*4
			key.Curve[curveIndex] = math.Float32frombits(
				binary.BigEndian.Uint32(payload[curveOffset:]),
			)
			if math.IsNaN(float64(key.Curve[curveIndex])) ||
				math.IsInf(float64(key.Curve[curveIndex]), 0) {
				return nil, 0, offset, false
			}
		}
		copy(key.Flags[:], payload[next+24:next+29])
		keys = append(keys, key)
		cursor = next + 29
	}
	return keys, keyReference, cursor, true
}

func renameProjectAnimationRecord(
	payload []byte,
	start int,
	source string,
	target string,
) ([]byte, error) {
	existing, err := projectStringOffsets(payload, target)
	if err != nil {
		return nil, fmt.Errorf("targetAnimation: %w", err)
	}
	if len(existing) != 0 {
		return nil, fmt.Errorf("target animation already exists: %s", target)
	}
	sourceName, err := encodeProjectString(source)
	if err != nil {
		return nil, err
	}
	targetName, err := encodeProjectString(target)
	if err != nil {
		return nil, err
	}
	if start < 0 || start+len(sourceName) > len(payload) ||
		!bytes.Equal(payload[start:start+len(sourceName)], sourceName) {
		return nil, &ParseError{
			Code: ErrInvalidProject,
			Msg:  "animation record does not start with its encoded name",
		}
	}
	renamed := make([]byte, 0, len(payload)+len(targetName)-len(sourceName))
	renamed = append(renamed, payload[:start]...)
	renamed = append(renamed, targetName...)
	renamed = append(renamed, payload[start+len(sourceName):]...)
	return renamed, nil
}
