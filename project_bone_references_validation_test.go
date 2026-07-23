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

// TestProjectBoneReferencesAgainstOfficialSamples is an opt-in integration
// test. Set SPINE233_BASELINE_EXPORTS to the validation baseline-exports
// directory. Set SPINE233_OFFICIAL_EXAMPLES as well to compare every decoded
// transform timeline with the official Pro JSON exports. No licensed Spine
// project is copied into this repository.
func TestProjectBoneReferencesAgainstOfficialSamples(t *testing.T) {
	root := os.Getenv("SPINE233_BASELINE_EXPORTS")
	if root == "" {
		t.Skip("SPINE233_BASELINE_EXPORTS is not set")
	}
	examplesRoot := os.Getenv("SPINE233_OFFICIAL_EXAMPLES")
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
	for _, sample := range samples {
		t.Run(sample, func(t *testing.T) {
			matches, err := filepath.Glob(filepath.Join(
				root,
				sample,
				"diagnostics",
				"*.decoded.bin",
			))
			if err != nil || len(matches) != 1 {
				t.Fatalf("decoded baseline matches = %v, err = %v", matches, err)
			}
			payload, err := os.ReadFile(matches[0])
			if err != nil {
				t.Fatal(err)
			}
			directory, err := DiscoverProjectBones(payload)
			if err != nil {
				t.Fatal(err)
			}
			if !directory.ReferencesComplete {
				t.Fatalf("bone references incomplete: %#v", directory.Records)
			}
			byReference := make(map[int]string, len(directory.Records))
			last := 0
			for _, bone := range directory.Records {
				if bone.WireReference <= last {
					t.Fatalf(
						"non-increasing reference for %q: %d after %d",
						bone.Name,
						bone.WireReference,
						last,
					)
				}
				last = bone.WireReference
				byReference[bone.WireReference] = bone.Name
			}

			animations, err := DiscoverProjectAnimations(payload)
			if err != nil {
				t.Fatal(err)
			}
			groupCount := 0
			transformCount := 0
			for _, animation := range animations.Records {
				groups := discoverProjectBoneTimelineGroups(
					payload,
					animation.Offset,
					animation.EndOffset,
				)
				for _, group := range groups {
					groupCount++
					if _, exists := byReference[group.BoneReference]; !exists {
						t.Fatalf(
							"animation %q uses unknown bone reference %d",
							animation.Name,
							group.BoneReference,
						)
					}
				}
			}
			if examplesRoot != "" {
				transformCount = validateProjectBoneNamesAgainstExport(
					t,
					payload,
					directory,
					animations,
					filepath.Join(examplesRoot, sample, "export"),
				)
			}
			t.Logf(
				"%d bones, %d animations, %d bone timeline groups, %d matched transform timelines",
				len(directory.Records),
				len(animations.Records),
				groupCount,
				transformCount,
			)
		})
	}
}

type officialProjectAnimation struct {
	Bones map[string]map[string][]map[string]any `json:"bones"`
}

type officialProjectExport struct {
	Animations map[string]officialProjectAnimation `json:"animations"`
}

func validateProjectBoneNamesAgainstExport(
	t *testing.T,
	payload []byte,
	bones *ProjectBoneDirectory,
	animations *ProjectAnimationDirectory,
	exportDirectory string,
) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(exportDirectory, "*.json"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("official export matches = %v, err = %v", matches, err)
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
		t.Fatalf("official Pro export is ambiguous: %v", matches)
	}
	encoded, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var official officialProjectExport
	if err := json.Unmarshal(encoded, &official); err != nil {
		t.Fatal(err)
	}

	usedAnimations := make(map[string]struct{}, len(official.Animations))
	matchedTimelines := 0
	for _, animation := range animations.Records {
		timelines := projectTransformTimelinesForRecord(payload, animation)
		if len(timelines) == 0 {
			continue
		}
		candidateNames := make([]string, 0, 2)
		for name := range official.Animations {
			if name == animation.Name || path.Base(name) == animation.Name {
				if _, used := usedAnimations[name]; !used {
					candidateNames = append(candidateNames, name)
				}
			}
		}
		if len(candidateNames) == 0 {
			continue
		}
		bestName := ""
		bestMatches := -1
		for _, name := range candidateNames {
			count := countMatchingProjectTransformTimelines(
				timelines,
				bones,
				official.Animations[name],
			)
			if count > bestMatches {
				bestName = name
				bestMatches = count
			}
		}
		if bestName == "" || bestMatches != len(timelines) {
			if bestName != "" {
				logUnmatchedProjectTransformTimelines(
					t,
					timelines,
					bones,
					official.Animations[bestName],
				)
			}
			t.Fatalf(
				"animation %q matched %d/%d transform timelines; candidates %v",
				animation.Name,
				bestMatches,
				len(timelines),
				candidateNames,
			)
		}
		usedAnimations[bestName] = struct{}{}
		matchedTimelines += bestMatches
	}
	officialTimelineCount := 0
	for name, animation := range official.Animations {
		count := officialProjectTransformTimelineCount(animation)
		officialTimelineCount += count
		if count > 0 {
			if _, used := usedAnimations[name]; !used {
				t.Fatalf("official animation %q was not matched", name)
			}
		}
	}
	if matchedTimelines != officialTimelineCount {
		t.Fatalf(
			"matched %d transform timelines, official export has %d",
			matchedTimelines,
			officialTimelineCount,
		)
	}
	return matchedTimelines
}

func officialProjectTransformTimelineCount(
	animation officialProjectAnimation,
) int {
	count := 0
	for _, timelines := range animation.Bones {
		for _, timelineType := range []string{
			ProjectTimelineRotate,
			ProjectTimelineTranslate,
			ProjectTimelineScale,
			ProjectTimelineShear,
		} {
			if _, exists := timelines[timelineType]; exists {
				count++
			}
		}
	}
	return count
}

func logUnmatchedProjectTransformTimelines(
	t *testing.T,
	timelines []ProjectTransformTimeline,
	bones *ProjectBoneDirectory,
	official officialProjectAnimation,
) {
	t.Helper()
	for _, timeline := range timelines {
		boneName, ok := bones.BoneNameByWireReference(timeline.BoneReference)
		if !ok {
			t.Logf("unresolved bone ref %d", timeline.BoneReference)
			continue
		}
		keys := official.Bones[boneName][timeline.Type]
		if !projectTransformKeysMatchExport(timeline, keys) {
			t.Logf(
				"unmatched ref=%d bone=%q type=%s projectKeys=%v exportKeys=%v",
				timeline.BoneReference,
				boneName,
				timeline.Type,
				timeline.Keys,
				keys,
			)
		}
	}
}

func projectTransformTimelinesForRecord(
	payload []byte,
	animation ProjectAnimationRecord,
) []ProjectTransformTimeline {
	groups := discoverProjectBoneTimelineGroups(
		payload,
		animation.Offset,
		animation.EndOffset,
	)
	timelines := make([]ProjectTransformTimeline, 0)
	for index, group := range groups {
		end := animation.EndOffset
		if index+1 < len(groups) {
			end = groups[index+1].Offset
		}
		timelines = append(
			timelines,
			discoverProjectTransformTimelinesInGroup(
				payload,
				group.Offset,
				end,
				group.BoneReference,
			)...,
		)
	}
	return timelines
}

func countMatchingProjectTransformTimelines(
	timelines []ProjectTransformTimeline,
	bones *ProjectBoneDirectory,
	official officialProjectAnimation,
) int {
	matched := 0
	for _, timeline := range timelines {
		boneName, ok := bones.BoneNameByWireReference(timeline.BoneReference)
		if !ok {
			continue
		}
		byType, exists := official.Bones[boneName]
		if !exists {
			continue
		}
		keys, exists := byType[timeline.Type]
		if !exists || !projectTransformKeysMatchExport(timeline, keys) {
			continue
		}
		matched++
	}
	return matched
}

func projectTransformKeysMatchExport(
	timeline ProjectTransformTimeline,
	exportKeys []map[string]any,
) bool {
	if len(timeline.Keys) != len(exportKeys) {
		return false
	}
	for index, key := range timeline.Keys {
		exportKey := exportKeys[index]
		if !closeProjectExportFloat(
			float64(key.Frame),
			projectExportNumber(exportKey, "time", 0)*projectAnimationFrameRate,
		) {
			return false
		}
		defaultValue := 0.0
		if timeline.Type == ProjectTimelineScale {
			defaultValue = 1
		}
		for component, channel := range timeline.Channels {
			if component >= len(key.Values) ||
				!closeProjectExportFloat(
					float64(key.Values[component]),
					projectExportNumber(exportKey, channel, defaultValue),
				) {
				return false
			}
		}
	}
	return true
}

func projectExportNumber(
	values map[string]any,
	name string,
	fallback float64,
) float64 {
	value, exists := values[name]
	if !exists {
		return fallback
	}
	number, ok := value.(float64)
	if !ok {
		return fallback
	}
	return number
}

func closeProjectExportFloat(left float64, right float64) bool {
	scale := math.Max(math.Abs(left), math.Abs(right))
	tolerance := math.Max(0.01, 0.00002*scale)
	return math.Abs(left-right) <= tolerance
}
