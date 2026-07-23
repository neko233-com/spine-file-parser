package spineparser

import "testing"

func TestDiscoverAndPatchProjectTransformTimelines(t *testing.T) {
	payload := projectTransformPayloadForTest()

	directory, err := DiscoverProjectTransformTimelines(payload, "attack")
	if err != nil {
		t.Fatal(err)
	}
	if len(directory.Timelines) != 4 {
		t.Fatalf("directory = %#v", directory)
	}
	translate := directory.Timelines[1]
	if translate.Type != ProjectTimelineTranslate ||
		len(translate.Keys) != 2 ||
		translate.Keys[1].Frame != 4 ||
		translate.Keys[1].Values[0] != float32(4.86) ||
		translate.Keys[1].Values[1] != float32(-0.24) {
		t.Fatalf("translate = %#v", translate)
	}

	document := &ProjectDocument{Payload: payload}
	patched, report, err := PatchProjectTransformValues(
		document,
		ProjectTransformPatch{
			Animation:       "attack",
			TargetAnimation: "attack-agent",
			Edits: []ProjectTransformValueEdit{
				{
					BoneReference: 6,
					Timeline:      ProjectTimelineTranslate,
					KeyIndex:      1,
					Channel:       "x",
					From:          4.86,
					To:            8,
				},
				{
					BoneReference: 6,
					Timeline:      ProjectTimelineScale,
					KeyIndex:      0,
					Channel:       "y",
					From:          1.1,
					To:            1.5,
				},
				{
					BoneReference: 6,
					Timeline:      ProjectTimelineTranslate,
					KeyIndex:      1,
					Channel:       "frame",
					From:          4,
					To:            5,
				},
				{
					BoneReference: 6,
					Timeline:      ProjectTimelineTranslate,
					KeyIndex:      1,
					Channel:       "curve.x.0",
					From:          float32(1 << 31),
					To:            1.25,
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 4 {
		t.Fatalf("report = %#v", report)
	}
	rediscovered, err := DiscoverProjectTransformTimelines(
		patched.Payload,
		"attack-agent",
	)
	if err != nil {
		t.Fatal(err)
	}
	if rediscovered.Timelines[1].Keys[1].Values[0] != 8 ||
		rediscovered.Timelines[1].Keys[1].Frame != 5 ||
		rediscovered.Timelines[1].Keys[1].Curves[0][0] != 1.25 ||
		rediscovered.Timelines[2].Keys[0].Values[1] != 1.5 {
		t.Fatalf("rediscovered = %#v", rediscovered.Timelines)
	}

	_, _, err = PatchProjectTransformValues(
		document,
		ProjectTransformPatch{
			Animation: "attack",
			Edits: []ProjectTransformValueEdit{
				{
					BoneReference: 6,
					Timeline:      ProjectTimelineTranslate,
					KeyIndex:      1,
					Channel:       "frame",
					From:          4,
					To:            0,
				},
			},
		},
	)
	if err == nil {
		t.Fatal("expected non-increasing frame error")
	}
}

func TestProjectTransformBoneNameGuard(t *testing.T) {
	payload := namedProjectTransformPayloadForTest(false)
	directory, err := DiscoverProjectTransformTimelines(payload, "attack")
	if err != nil {
		t.Fatal(err)
	}
	for _, timeline := range directory.Timelines {
		if timeline.BoneReference != 6 || timeline.BoneName != "hand" {
			t.Fatalf("timeline bone identity = %#v", timeline)
		}
	}

	document := &ProjectDocument{Payload: payload}
	_, report, err := PatchProjectTransformValues(
		document,
		ProjectTransformPatch{
			Animation: "attack",
			Edits: []ProjectTransformValueEdit{{
				BoneReference: 6,
				BoneName:      "hand",
				Timeline:      ProjectTimelineTranslate,
				KeyIndex:      1,
				Channel:       "x",
				From:          4.86,
				To:            8,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 1 || report.Changes[0].BoneName != "hand" {
		t.Fatalf("report = %#v", report)
	}

	_, _, err = PatchProjectTransformValues(
		document,
		ProjectTransformPatch{
			Animation: "attack",
			Edits: []ProjectTransformValueEdit{{
				BoneReference: 6,
				BoneName:      "body",
				Timeline:      ProjectTimelineTranslate,
				KeyIndex:      1,
				Channel:       "x",
				From:          4.86,
				To:            8,
			}},
		},
	)
	if err == nil {
		t.Fatal("expected bone name/reference mismatch")
	}
}

func TestProjectTransformBoneNameIncompleteMapping(t *testing.T) {
	payload := namedProjectTransformPayloadForTest(true)
	directory, err := DiscoverProjectTransformTimelines(payload, "attack")
	if err != nil {
		t.Fatal(err)
	}
	for _, timeline := range directory.Timelines {
		if timeline.BoneName != "" {
			t.Fatalf("unproved bone name was exposed: %#v", timeline)
		}
	}

	document := &ProjectDocument{Payload: payload}
	_, _, err = PatchProjectTransformValues(
		document,
		ProjectTransformPatch{
			Animation: "attack",
			Edits: []ProjectTransformValueEdit{{
				BoneReference: 6,
				BoneName:      "hand",
				Timeline:      ProjectTimelineTranslate,
				KeyIndex:      1,
				Channel:       "x",
				From:          4.86,
				To:            8,
			}},
		},
	)
	if err == nil {
		t.Fatal("expected unproved bone name rejection")
	}

	_, report, err := PatchProjectTransformValues(
		document,
		ProjectTransformPatch{
			Animation: "attack",
			Edits: []ProjectTransformValueEdit{{
				BoneReference: 6,
				Timeline:      ProjectTimelineTranslate,
				KeyIndex:      1,
				Channel:       "x",
				From:          4.86,
				To:            8,
			}},
		},
	)
	if err != nil {
		t.Fatalf("ref-only compatibility: %v", err)
	}
	if len(report.Changes) != 1 || report.Changes[0].BoneName != "" {
		t.Fatalf("unproved report name = %#v", report)
	}
}

func projectTransformPayloadForTest() []byte {
	payload := append([]byte{}, modernAnimationHeaderPrefix...)
	payload = append(payload, 0x01)
	payload = append(payload, modernAnimationHeaderSuffix...)
	payload = append(payload, 0x09)
	payload = append(payload, modernAnimationHeaderTail...)
	payload = append(payload, kryoASCIIForTest("attack")...)
	payload = append(payload, modernAnimationValuePrefix...)
	payload = append(payload, projectBoneTimelineGroupPrefix...)
	payload = appendPositiveVarintForTest(payload, 731)
	payload = append(payload, 0x01, 0x06)
	payload = append(payload, projectBoneTimelineMapPrefix...)
	payload = append(payload, 0x04)
	payload = appendTransformTimelineForTest(
		payload,
		272,
		0,
		[][]float32{{0, 2.2}, {2, 13.22}},
	)
	payload = appendTransformTimelineForTest(
		payload,
		280,
		1,
		[][]float32{{0, -0.77, -1.89}, {4, 4.86, -0.24}},
	)
	payload = appendTransformTimelineForTest(
		payload,
		290,
		2,
		[][]float32{{0, 1, 1.1}},
	)
	payload = appendTransformTimelineForTest(
		payload,
		300,
		3,
		[][]float32{{0, 3, 4}},
	)

	return payload
}

func namedProjectTransformPayloadForTest(
	contradictBoneReferences bool,
) []byte {
	payload := []byte{0x55}
	payload = append(payload, projectBoneTablePrefix...)
	payload = append(payload, 0x03, 0x0c)
	payload = append(payload, projectBoneRecordForReferenceTest("root", 0)...)
	payload = append(payload, 0x7e, 0x00, 0x0c)
	payload = append(payload, projectBoneRecordForReferenceTest("body", 4)...)
	payload = append(payload, 0x7e, 0x00, 0x0c)
	payload = append(payload, projectBoneRecordForReferenceTest("hand", 5)...)
	payload = append(payload, 0x7e, 0x00)
	if contradictBoneReferences {
		payload = append(payload, projectSlotRecordPrefix...)
		payload = append(payload, kryoASCIIForTest("contradiction")...)
		payload = append(payload, 0x02, 0x63)
	}
	return append(payload, projectTransformPayloadForTest()...)
}

func appendTransformTimelineForTest(
	output []byte,
	timelineReference int,
	timelineType byte,
	frames [][]float32,
) []byte {
	output = append(output, projectTimelinePrefix...)
	output = appendPositiveVarintForTest(output, timelineReference)
	output = append(output, timelineType, 0x01)
	output = appendPositiveVarintForTest(output, len(frames))
	componentCount := 1
	if timelineType != 0 {
		componentCount = 2
	}
	for _, frame := range frames {
		output = append(output, projectTimelineKeyPrefix...)
		output = appendPositiveVarintForTest(output, timelineReference)
		output = appendPositiveVarintForTest(output, timelineReference+1)
		for _, value := range frame {
			output = appendFloat32ForTest(output, value)
		}
		for component := 0; component < componentCount; component++ {
			for range 4 {
				output = appendFloat32ForTest(output, float32(1<<31))
			}
			flagCount := 4
			if component == componentCount-1 {
				flagCount = 5
			}
			for range flagCount {
				output = append(output, 0)
			}
		}
	}
	return output
}
