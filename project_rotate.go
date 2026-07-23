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
	transforms, err := DiscoverProjectTransformTimelines(payload, animation)
	if err != nil {
		return nil, err
	}
	timelines := make([]ProjectRotateTimeline, 0)
	for _, transform := range transforms.Timelines {
		if transform.Type != ProjectTimelineRotate {
			continue
		}
		keys := make([]ProjectRotateKey, 0, len(transform.Keys))
		for _, key := range transform.Keys {
			var flags [5]byte
			copy(flags[:], key.CurveFlags)
			keys = append(keys, ProjectRotateKey{
				Index:       key.Index,
				Frame:       key.Frame,
				Time:        key.Time,
				Value:       key.Values[0],
				Offset:      key.Offset,
				ValueOffset: key.ValueOffsets[0],
				Curve:       key.Curves[0],
				Flags:       flags,
			})
		}
		timelines = append(timelines, ProjectRotateTimeline{
			BoneReference:     transform.BoneReference,
			TimelineReference: transform.TimelineReference,
			KeyReference:      transform.KeyReference,
			Offset:            transform.Offset,
			Keys:              keys,
		})
	}
	if len(timelines) == 0 {
		return nil, &ParseError{
			Code: ErrInvalidProject,
			Msg:  fmt.Sprintf("animation %q contains no supported rotate timelines", animation),
		}
	}
	return &ProjectRotateTimelineDirectory{
		Animation:   animation,
		RegionStart: transforms.RegionStart,
		RegionEnd:   transforms.RegionEnd,
		FrameRate:   transforms.FrameRate,
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
