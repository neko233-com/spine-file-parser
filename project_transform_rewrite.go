package spineparser

import (
	"fmt"
	"math"
	"strings"
)

// ProjectTransformKeySpec declares the complete editable state for one
// existing transform key. Curves may be omitted to preserve them.
type ProjectTransformKeySpec struct {
	Frame  float32      `json:"frame"`
	Values []float32    `json:"values"`
	Curves [][4]float32 `json:"curves,omitempty"`
}

// ProjectTransformTimelineRewrite declares every key for one existing
// transform timeline. Key count and channel count must remain unchanged.
type ProjectTransformTimelineRewrite struct {
	BoneReference int                       `json:"boneReference"`
	Timeline      string                    `json:"timeline"`
	Keys          []ProjectTransformKeySpec `json:"keys"`
}

// ProjectTransformRewrite controls declarative, fixed-topology animation
// rewriting and optional animation renaming.
type ProjectTransformRewrite struct {
	Animation       string                            `json:"animation"`
	TargetAnimation string                            `json:"targetAnimation,omitempty"`
	Timelines       []ProjectTransformTimelineRewrite `json:"timelines"`
}

// RewriteProjectTransformTimelines validates complete timeline declarations,
// converts differences to fail-closed semantic edits, and clones the document.
func RewriteProjectTransformTimelines(
	document *ProjectDocument,
	rewrite ProjectTransformRewrite,
) (*ProjectDocument, ProjectTransformPatchReport, error) {
	if document == nil || len(document.Payload) == 0 {
		return nil, ProjectTransformPatchReport{},
			&ParseError{Code: ErrInvalidInput, Msg: "project payload is empty"}
	}
	if len(rewrite.Timelines) == 0 {
		return nil, ProjectTransformPatchReport{},
			&ParseError{Code: ErrInvalidInput, Msg: "at least one timeline rewrite is required"}
	}
	directory, err := DiscoverProjectTransformTimelines(
		document.Payload,
		rewrite.Animation,
	)
	if err != nil {
		return nil, ProjectTransformPatchReport{}, err
	}
	type timelineKey struct {
		BoneReference int
		Type          string
	}
	available := make(map[timelineKey][]ProjectTransformTimeline)
	for _, timeline := range directory.Timelines {
		key := timelineKey{
			BoneReference: timeline.BoneReference,
			Type:          timeline.Type,
		}
		available[key] = append(available[key], timeline)
	}

	seen := make(map[timelineKey]struct{}, len(rewrite.Timelines))
	edits := make([]ProjectTransformValueEdit, 0)
	for rewriteIndex, requested := range rewrite.Timelines {
		timelineType := strings.ToLower(strings.TrimSpace(requested.Timeline))
		key := timelineKey{
			BoneReference: requested.BoneReference,
			Type:          timelineType,
		}
		if _, exists := seen[key]; exists {
			return nil, ProjectTransformPatchReport{}, fmt.Errorf(
				"timeline rewrite %d: duplicate boneReference/timeline",
				rewriteIndex,
			)
		}
		seen[key] = struct{}{}
		matches := available[key]
		if len(matches) == 0 {
			return nil, ProjectTransformPatchReport{}, fmt.Errorf(
				"timeline rewrite %d: %s timeline not found for boneReference %d",
				rewriteIndex,
				timelineType,
				requested.BoneReference,
			)
		}
		if len(matches) != 1 {
			return nil, ProjectTransformPatchReport{}, fmt.Errorf(
				"timeline rewrite %d: boneReference %d matched %d %s timelines",
				rewriteIndex,
				requested.BoneReference,
				len(matches),
				timelineType,
			)
		}
		current := matches[0]
		if len(requested.Keys) != len(current.Keys) {
			return nil, ProjectTransformPatchReport{}, fmt.Errorf(
				"timeline rewrite %d: key count is %d, expected %d",
				rewriteIndex,
				len(requested.Keys),
				len(current.Keys),
			)
		}
		for keyIndex, requestedKey := range requested.Keys {
			if !finiteProjectFloat(requestedKey.Frame) || requestedKey.Frame < 0 {
				return nil, ProjectTransformPatchReport{}, fmt.Errorf(
					"timeline rewrite %d key %d: frame must be finite and non-negative",
					rewriteIndex,
					keyIndex,
				)
			}
			if len(requestedKey.Values) != len(current.Channels) {
				return nil, ProjectTransformPatchReport{}, fmt.Errorf(
					"timeline rewrite %d key %d: value count is %d, expected %d",
					rewriteIndex,
					keyIndex,
					len(requestedKey.Values),
					len(current.Channels),
				)
			}
			if len(requestedKey.Curves) != 0 &&
				len(requestedKey.Curves) != len(current.Channels) {
				return nil, ProjectTransformPatchReport{}, fmt.Errorf(
					"timeline rewrite %d key %d: curve channel count is %d, expected %d",
					rewriteIndex,
					keyIndex,
					len(requestedKey.Curves),
					len(current.Channels),
				)
			}
			existingKey := current.Keys[keyIndex]
			if math.Float32bits(requestedKey.Frame) !=
				math.Float32bits(existingKey.Frame) {
				edits = append(edits, ProjectTransformValueEdit{
					BoneReference: requested.BoneReference,
					Timeline:      timelineType,
					KeyIndex:      keyIndex,
					Channel:       "frame",
					From:          existingKey.Frame,
					To:            requestedKey.Frame,
				})
			}
			for channelIndex, channel := range current.Channels {
				value := requestedKey.Values[channelIndex]
				if !finiteProjectFloat(value) {
					return nil, ProjectTransformPatchReport{}, fmt.Errorf(
						"timeline rewrite %d key %d channel %s: value must be finite",
						rewriteIndex,
						keyIndex,
						channel,
					)
				}
				if math.Float32bits(value) !=
					math.Float32bits(existingKey.Values[channelIndex]) {
					edits = append(edits, ProjectTransformValueEdit{
						BoneReference: requested.BoneReference,
						Timeline:      timelineType,
						KeyIndex:      keyIndex,
						Channel:       channel,
						From:          existingKey.Values[channelIndex],
						To:            value,
					})
				}
				if len(requestedKey.Curves) == 0 {
					continue
				}
				for curveIndex, curveValue := range requestedKey.Curves[channelIndex] {
					if !finiteProjectFloat(curveValue) {
						return nil, ProjectTransformPatchReport{}, fmt.Errorf(
							"timeline rewrite %d key %d curve %s.%d: value must be finite",
							rewriteIndex,
							keyIndex,
							channel,
							curveIndex,
						)
					}
					if math.Float32bits(curveValue) ==
						math.Float32bits(existingKey.Curves[channelIndex][curveIndex]) {
						continue
					}
					edits = append(edits, ProjectTransformValueEdit{
						BoneReference: requested.BoneReference,
						Timeline:      timelineType,
						KeyIndex:      keyIndex,
						Channel: fmt.Sprintf(
							"curve.%s.%d",
							channel,
							curveIndex,
						),
						From: existingKey.Curves[channelIndex][curveIndex],
						To:   curveValue,
					})
				}
			}
		}
	}
	if len(edits) == 0 {
		return nil, ProjectTransformPatchReport{},
			&ParseError{Code: ErrInvalidInput, Msg: "timeline rewrite contains no changes"}
	}
	return PatchProjectTransformValues(document, ProjectTransformPatch{
		Animation:       rewrite.Animation,
		TargetAnimation: rewrite.TargetAnimation,
		Edits:           edits,
	})
}
