package spineparser

import (
	"encoding/json"
)

// SkeletonInfo is the exported Spine JSON metadata block.
type SkeletonInfo struct {
	Hash   string  `json:"hash,omitempty"`
	Spine  string  `json:"spine,omitempty"`
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	Width  float64 `json:"width,omitempty"`
	Height float64 `json:"height,omitempty"`
	FPS    float64 `json:"fps,omitempty"`
	Images string  `json:"images,omitempty"`
	Audio  string  `json:"audio,omitempty"`
}

// Bone is an exported skeleton bone. Data retains all version-specific fields.
type Bone struct {
	Name   string         `json:"name"`
	Parent string         `json:"parent,omitempty"`
	Data   map[string]any `json:"-"`
}

// Slot is an exported skeleton slot. Data retains all version-specific fields.
type Slot struct {
	Name string         `json:"name"`
	Bone string         `json:"bone"`
	Data map[string]any `json:"-"`
}

// SpineJSON is typed where stable and keeps full raw JSON for version-specific data.
type SpineJSON struct {
	Skeleton   *SkeletonInfo              `json:"skeleton,omitempty"`
	Bones      []Bone                     `json:"bones,omitempty"`
	Slots      []Slot                     `json:"slots,omitempty"`
	Skins      json.RawMessage            `json:"skins,omitempty"`
	Events     map[string]json.RawMessage `json:"events,omitempty"`
	Animations map[string]json.RawMessage `json:"animations,omitempty"`
	Raw        map[string]json.RawMessage `json:"-"`
}

func unmarshalObjectData(data []byte) (map[string]any, error) {
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (b *Bone) UnmarshalJSON(data []byte) error {
	type stable Bone
	var parsed stable
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	all, err := unmarshalObjectData(data)
	if err != nil {
		return err
	}
	*b = Bone(parsed)
	b.Data = all
	return nil
}

func (s *Slot) UnmarshalJSON(data []byte) error {
	type stable Slot
	var parsed stable
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	all, err := unmarshalObjectData(data)
	if err != nil {
		return err
	}
	*s = Slot(parsed)
	s.Data = all
	return nil
}

// ParseJSON parses standard Spine skeleton JSON.
func ParseJSON(source []byte) (*SpineJSON, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(source, &raw); err != nil {
		return nil, &ParseError{Code: ErrInvalidJSON, Msg: "invalid Spine JSON", Cause: err}
	}
	if raw == nil {
		return nil, &ParseError{Code: ErrInvalidJSON, Msg: "Spine JSON root must be an object"}
	}

	var parsed SpineJSON
	if err := json.Unmarshal(source, &parsed); err != nil {
		return nil, &ParseError{Code: ErrInvalidJSON, Msg: "invalid Spine JSON structure", Cause: err}
	}
	for _, bone := range parsed.Bones {
		if bone.Name == "" {
			return nil, &ParseError{Code: ErrInvalidJSON, Msg: "Spine JSON contains a bone without a name"}
		}
	}
	parsed.Raw = raw
	return &parsed, nil
}
