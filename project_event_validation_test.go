package spineparser

import (
	"encoding/json"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectEventTimelinesAgainstOfficialSamples is an opt-in integration
// test. It validates every event and every event-free animation in the modern
// official sample set without copying licensed projects into this repository.
func TestProjectEventTimelinesAgainstOfficialSamples(t *testing.T) {
	baselines := os.Getenv("SPINE233_BASELINE_EXPORTS")
	examples := os.Getenv("SPINE233_OFFICIAL_EXAMPLES")
	if baselines == "" || examples == "" {
		t.Skip("SPINE233_BASELINE_EXPORTS and SPINE233_OFFICIAL_EXAMPLES are required")
	}
	samples := []string{
		"alien",
		"celestial-circus",
		"chibi-stickers",
		"hero",
		"mix-and-match",
		"owl",
		"raptor",
		"spineboy",
		"stretchyman",
	}
	totalTimelines := 0
	totalKeys := 0
	for _, sample := range samples {
		t.Run(sample, func(t *testing.T) {
			payload := readOfficialProjectPayloadForEventTest(
				t,
				filepath.Join(baselines, sample, "diagnostics"),
			)
			official := readOfficialProjectEventsForTest(
				t,
				filepath.Join(examples, sample, "export"),
			)
			animations, err := DiscoverProjectAnimations(payload)
			if err != nil {
				t.Fatal(err)
			}
			matched := make(map[string]struct{})
			sampleTimelines := 0
			sampleKeys := 0
			for _, animation := range animations.Records {
				officialName, expected, found :=
					matchOfficialProjectEventsForTest(
						t,
						official,
						animation.Name,
					)
				directory, discoverErr := DiscoverProjectEventTimelines(
					payload,
					animation.Name,
				)
				if !found || len(expected) == 0 {
					if discoverErr == nil {
						t.Fatalf(
							"animation %q produced false event timeline %#v",
							animation.Name,
							directory,
						)
					}
					continue
				}
				if discoverErr != nil {
					t.Fatalf("animation %q: %v", animation.Name, discoverErr)
				}
				if len(directory.Timelines) != 1 {
					t.Fatalf(
						"animation %q event timelines = %#v",
						animation.Name,
						directory.Timelines,
					)
				}
				keys := directory.Timelines[0].Keys
				if len(keys) != len(expected) {
					t.Fatalf(
						"animation %q event keys = %d, expected %d",
						animation.Name,
						len(keys),
						len(expected),
					)
				}
				for index, key := range keys {
					expectedFrame := expected[index].Time *
						projectAnimationFrameRate
					if math.Abs(float64(key.Frame)-expectedFrame) > 0.02 {
						t.Fatalf(
							"animation %q key %d frame = %v, expected %v",
							animation.Name,
							index,
							key.Frame,
							expectedFrame,
						)
					}
				}
				verifyOfficialProjectEventPatchForTest(
					t,
					payload,
					animation.Name,
					directory.Timelines[0],
				)
				matched[officialName] = struct{}{}
				sampleTimelines++
				sampleKeys += len(keys)
			}
			for name, animation := range official.Animations {
				if len(animation.Events) == 0 {
					continue
				}
				if _, exists := matched[name]; !exists {
					t.Fatalf("official event animation %q was not matched", name)
				}
			}
			totalTimelines += sampleTimelines
			totalKeys += sampleKeys
			t.Logf("%d event timelines, %d keys", sampleTimelines, sampleKeys)
		})
	}
	if totalTimelines != 8 || totalKeys != 15 {
		t.Fatalf(
			"official event totals = %d timelines/%d keys, expected 8/15",
			totalTimelines,
			totalKeys,
		)
	}
}

func verifyOfficialProjectEventPatchForTest(
	t *testing.T,
	payload []byte,
	animation string,
	timeline ProjectEventTimeline,
) {
	t.Helper()
	key := timeline.Keys[0]
	target := key.Frame + 0.25
	if len(timeline.Keys) > 1 && target >= timeline.Keys[1].Frame {
		target = (key.Frame + timeline.Keys[1].Frame) / 2
	}
	patched, report, err := PatchProjectEventFrames(
		&ProjectDocument{Payload: payload},
		ProjectEventPatch{
			Animation: animation,
			Edits: []ProjectEventFrameEdit{{
				TimelineReference: timeline.TimelineReference,
				KeyReference:      timeline.KeyReference,
				TimelineOffset:    timeline.Offset,
				KeyIndex:          0,
				From:              key.Frame,
				To:                target,
			}},
		},
	)
	if err != nil {
		t.Fatalf("patch animation %q: %v", animation, err)
	}
	if len(report.Changes) != 1 {
		t.Fatalf("patch animation %q report = %#v", animation, report)
	}
	rediscovered, err := DiscoverProjectEventTimelines(
		patched.Payload,
		animation,
	)
	if err != nil {
		t.Fatalf("rediscover animation %q: %v", animation, err)
	}
	if rediscovered.Timelines[0].Keys[0].Frame != target {
		t.Fatalf(
			"animation %q patched frame = %v, expected %v",
			animation,
			rediscovered.Timelines[0].Keys[0].Frame,
			target,
		)
	}
	for offset := range payload {
		if offset >= key.FrameOffset && offset < key.FrameOffset+4 {
			continue
		}
		if payload[offset] != patched.Payload[offset] {
			t.Fatalf(
				"animation %q changed byte %d outside target frame",
				animation,
				offset,
			)
		}
	}
}

type officialProjectEventKeyForTest struct {
	Time float64 `json:"time"`
	Name string  `json:"name"`
}

type officialProjectEventAnimationForTest struct {
	Events []officialProjectEventKeyForTest `json:"events"`
}

type officialProjectEventsForTest struct {
	Animations map[string]officialProjectEventAnimationForTest `json:"animations"`
}

func readOfficialProjectPayloadForEventTest(
	t *testing.T,
	diagnostics string,
) []byte {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(
		diagnostics,
		"*.decoded.bin",
	))
	if err != nil || len(matches) != 1 {
		t.Fatalf("decoded project matches = %v, err = %v", matches, err)
	}
	payload, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func readOfficialProjectEventsForTest(
	t *testing.T,
	exportDirectory string,
) officialProjectEventsForTest {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(exportDirectory, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	exportPath := ""
	for _, match := range matches {
		if strings.HasSuffix(
			strings.ToLower(filepath.Base(match)),
			"-pro.json",
		) {
			exportPath = match
			break
		}
	}
	if exportPath == "" && len(matches) == 1 {
		exportPath = matches[0]
	}
	if exportPath == "" {
		t.Fatalf("official Pro JSON was not found in %s", exportDirectory)
	}
	encoded, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var official officialProjectEventsForTest
	if err := json.Unmarshal(encoded, &official); err != nil {
		t.Fatal(err)
	}
	return official
}

func matchOfficialProjectEventsForTest(
	t *testing.T,
	official officialProjectEventsForTest,
	leaf string,
) (string, []officialProjectEventKeyForTest, bool) {
	t.Helper()
	matches := make([]string, 0, 1)
	for name := range official.Animations {
		if name == leaf || path.Base(name) == leaf {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return "", nil, false
	}
	eventMatches := make([]string, 0, 1)
	for _, name := range matches {
		if len(official.Animations[name].Events) > 0 {
			eventMatches = append(eventMatches, name)
		}
	}
	if len(eventMatches) > 1 {
		t.Fatalf("event animation leaf %q is ambiguous: %v", leaf, eventMatches)
	}
	if len(eventMatches) == 1 {
		name := eventMatches[0]
		return name, official.Animations[name].Events, true
	}
	return matches[0], nil, true
}
