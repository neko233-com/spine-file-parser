package spineparser

import "testing"

func TestRewriteProjectTransformTimelines(t *testing.T) {
	payload := projectTransformPayloadForTest()
	document := &ProjectDocument{Payload: payload}
	patched, report, err := RewriteProjectTransformTimelines(
		document,
		ProjectTransformRewrite{
			Animation:       "attack",
			TargetAnimation: "attack-agent",
			Timelines: []ProjectTransformTimelineRewrite{
				{
					BoneReference: 6,
					Timeline:      ProjectTimelineTranslate,
					Keys: []ProjectTransformKeySpec{
						{Frame: 0, Values: []float32{-0.77, -1.89}},
						{Frame: 5, Values: []float32{8, -0.24}},
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 2 {
		t.Fatalf("report = %#v", report)
	}
	directory, err := DiscoverProjectTransformTimelines(
		patched.Payload,
		"attack-agent",
	)
	if err != nil {
		t.Fatal(err)
	}
	translate := directory.Timelines[1]
	if translate.Keys[1].Frame != 5 ||
		translate.Keys[1].Values[0] != 8 ||
		translate.Keys[1].Values[1] != float32(-0.24) {
		t.Fatalf("translate = %#v", translate)
	}
}

func TestRewriteProjectTransformTimelinesRejectsTopologyChange(t *testing.T) {
	document := &ProjectDocument{Payload: projectTransformPayloadForTest()}
	_, _, err := RewriteProjectTransformTimelines(
		document,
		ProjectTransformRewrite{
			Animation: "attack",
			Timelines: []ProjectTransformTimelineRewrite{
				{
					BoneReference: 6,
					Timeline:      ProjectTimelineTranslate,
					Keys: []ProjectTransformKeySpec{
						{Frame: 0, Values: []float32{0, 0}},
					},
				},
			},
		},
	)
	if err == nil {
		t.Fatal("expected key-count error")
	}
}

func TestRewriteProjectTransformTimelineBoneNameGuard(t *testing.T) {
	document := &ProjectDocument{
		Payload: namedProjectTransformPayloadForTest(false),
	}
	_, report, err := RewriteProjectTransformTimelines(
		document,
		ProjectTransformRewrite{
			Animation: "attack",
			Timelines: []ProjectTransformTimelineRewrite{{
				BoneReference: 6,
				BoneName:      "hand",
				Timeline:      ProjectTimelineTranslate,
				Keys: []ProjectTransformKeySpec{
					{Frame: 0, Values: []float32{-0.77, -1.89}},
					{Frame: 4, Values: []float32{8, -0.24}},
				},
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 1 || report.Changes[0].BoneName != "hand" {
		t.Fatalf("report = %#v", report)
	}

	for _, test := range []struct {
		name    string
		payload []byte
		bone    string
	}{
		{
			name:    "mismatch",
			payload: namedProjectTransformPayloadForTest(false),
			bone:    "body",
		},
		{
			name:    "incomplete mapping",
			payload: namedProjectTransformPayloadForTest(true),
			bone:    "hand",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := RewriteProjectTransformTimelines(
				&ProjectDocument{Payload: test.payload},
				ProjectTransformRewrite{
					Animation: "attack",
					Timelines: []ProjectTransformTimelineRewrite{{
						BoneReference: 6,
						BoneName:      test.bone,
						Timeline:      ProjectTimelineTranslate,
						Keys: []ProjectTransformKeySpec{
							{Frame: 0, Values: []float32{-0.77, -1.89}},
							{Frame: 4, Values: []float32{8, -0.24}},
						},
					}},
				},
			)
			if err == nil {
				t.Fatal("expected bone name guard error")
			}
		})
	}
}
