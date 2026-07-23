package spineparser

import (
	"bytes"
	"sort"
)

const (
	projectFirstWireReference       = 4
	projectBoneOuterSolutionLimit   = 4096
	projectBoneMinimumWireReference = projectFirstWireReference
)

var (
	projectObjectTerminator = []byte{0x7e, 0x00}
	projectSlotRecordPrefix = []byte{0x01, 0x11, 0x00, 0x03, 0x01, 0x01}
)

type projectOuterBoneEvent struct {
	Offset      int
	RecordIndex int
	Reference   int
}

type projectBoneReferenceResolution struct {
	references []int
	complete   bool
	resolved   int
	laterNew   int
	events     []projectOuterBoneEvent
}

// resolveProjectBoneReferences reconstructs the object IDs assigned by
// Kryo's reference resolver. Spine's outer bone collection gives direct new
// objects, explicit references for bones first reached through another object,
// and (rarely) later new objects whose IDs are proved by bone-only references.
func resolveProjectBoneReferences(
	payload []byte,
	outerStart int,
	records []ProjectBoneRecord,
) ([]int, bool) {
	references := make([]int, len(records))
	if len(records) == 0 || outerStart < 0 || outerStart >= len(payload) {
		return references, false
	}

	directCount := projectDirectBoneCount(payload, records)
	for index := 0; index < directCount; index++ {
		references[index] = projectFirstWireReference + index
	}
	evidence := collectProjectBoneReferenceEvidence(payload, records)
	if directCount == len(records) {
		if projectBoneReferencesCoverEvidence(references, evidence) {
			return references, true
		}
		// The root reference is fixed by the enclosing project and bone-list
		// objects. Later direct-looking records are not exposed when another
		// bone-only reference contradicts the contiguous-ID assumption.
		references = make([]int, len(records))
		references[0] = projectFirstWireReference
		return references, false
	}

	recordByOffset := make(map[int]int, len(records))
	for index, record := range records {
		recordByOffset[record.Offset] = index
	}
	boneClassToken := payload[outerStart]
	boundaries := projectOuterBoneBoundaries(
		payload,
		boneClassToken,
		recordByOffset,
	)
	if events, ok := projectOuterReferenceTail(
		payload,
		boneClassToken,
		boundaries,
		records[directCount-1].Offset,
		len(records)-directCount,
		directCount,
	); ok {
		resolution := resolveProjectBoneEventReferences(
			records,
			directCount,
			events,
			evidence,
		)
		if resolution.complete {
			return resolution.references, true
		}
	}

	search := projectOuterBoneSearch{
		payload:        payload,
		boneClassToken: boneClassToken,
		recordByOffset: recordByOffset,
		records:        records,
		directCount:    directCount,
		boundaries:     boundaries,
		evidence:       evidence,
		seenRecords:    make(map[int]struct{}, len(records)),
		seenReferences: make(map[int]struct{}, len(records)),
	}
	for index := 0; index < directCount; index++ {
		search.seenRecords[index] = struct{}{}
	}
	search.searchAfterNew(records[directCount-1].Offset, directCount)

	if search.best.references == nil || !search.best.complete {
		return references, false
	}
	return search.best.references, true
}

// projectOuterReferenceTail handles the common complex layout: after a
// directly serialized prefix, every remaining collection item is an existing
// Bone reference. Requiring the whole remaining count as one contiguous class
// and reference run makes this both fast and unambiguous in large projects.
func projectOuterReferenceTail(
	payload []byte,
	boneClassToken byte,
	boundaries []int,
	after int,
	count int,
	directCount int,
) ([]projectOuterBoneEvent, bool) {
	for _, start := range boundaries {
		if start <= after {
			continue
		}
		events := make([]projectOuterBoneEvent, 0, count)
		seen := make(map[int]struct{}, count)
		cursor := start
		valid := true
		for index := 0; index < count; index++ {
			if cursor+1 >= len(payload) || payload[cursor] != boneClassToken {
				valid = false
				break
			}
			reference, next, ok := readPositiveVarint(payload, cursor+1)
			if !ok || reference < projectFirstWireReference+directCount {
				valid = false
				break
			}
			if _, exists := seen[reference]; exists {
				valid = false
				break
			}
			seen[reference] = struct{}{}
			events = append(events, projectOuterBoneEvent{
				Offset:      cursor,
				RecordIndex: -1,
				Reference:   reference,
			})
			cursor = next
		}
		if valid {
			return events, true
		}
	}
	return nil, false
}

func projectDirectBoneCount(
	payload []byte,
	records []ProjectBoneRecord,
) int {
	if len(records) == 0 {
		return 0
	}
	direct := 1
	for direct < len(records) {
		offset := records[direct].Offset
		if offset < 3 ||
			payload[offset-3] != projectObjectTerminator[0] ||
			payload[offset-2] != projectObjectTerminator[1] ||
			payload[offset-1] != 0x0c {
			break
		}
		direct++
	}
	return direct
}

func projectOuterBoneBoundaries(
	payload []byte,
	boneClassToken byte,
	recordByOffset map[int]int,
) []int {
	boundaries := make([]int, 0, 128)
	for offset := 2; offset+1 < len(payload); offset++ {
		if payload[offset-2] != projectObjectTerminator[0] ||
			payload[offset-1] != projectObjectTerminator[1] ||
			payload[offset] != boneClassToken {
			continue
		}
		token, _, ok := readPositiveVarint(payload, offset+1)
		if !ok {
			continue
		}
		if token == 1 {
			if _, exists := recordByOffset[offset+1]; !exists {
				continue
			}
		} else if token < projectBoneMinimumWireReference {
			continue
		}
		boundaries = append(boundaries, offset)
	}
	return boundaries
}

type projectOuterBoneSearch struct {
	payload        []byte
	boneClassToken byte
	recordByOffset map[int]int
	records        []ProjectBoneRecord
	directCount    int
	boundaries     []int
	evidence       map[int]struct{}
	seenRecords    map[int]struct{}
	seenReferences map[int]struct{}
	events         []projectOuterBoneEvent
	solutions      int
	stopped        bool
	best           projectBoneReferenceResolution
}

func (search *projectOuterBoneSearch) searchAfterNew(
	after int,
	eventIndex int,
) {
	if search.stopped {
		return
	}
	first := sort.SearchInts(search.boundaries, after+1)
	for index := first; index < len(search.boundaries); index++ {
		search.parseAt(search.boundaries[index], eventIndex)
		if search.stopped {
			return
		}
	}
}

func (search *projectOuterBoneSearch) parseAt(
	offset int,
	eventIndex int,
) {
	if search.stopped || eventIndex >= len(search.records) ||
		offset < 0 || offset+1 >= len(search.payload) ||
		search.payload[offset] != search.boneClassToken {
		return
	}
	token, cursor, ok := readPositiveVarint(search.payload, offset+1)
	if !ok {
		return
	}

	event := projectOuterBoneEvent{
		Offset:      offset,
		RecordIndex: -1,
	}
	if token == 1 {
		recordIndex, exists := search.recordByOffset[offset+1]
		if !exists || recordIndex < search.directCount {
			return
		}
		if _, exists := search.seenRecords[recordIndex]; exists {
			return
		}
		event.RecordIndex = recordIndex
		search.seenRecords[recordIndex] = struct{}{}
		defer delete(search.seenRecords, recordIndex)
	} else {
		if token < projectBoneMinimumWireReference ||
			token < projectFirstWireReference+search.directCount {
			return
		}
		if _, exists := search.seenReferences[token]; exists {
			return
		}
		event.Reference = token
		search.seenReferences[token] = struct{}{}
		defer delete(search.seenReferences, token)
	}

	search.events = append(search.events, event)
	defer func() {
		search.events = search.events[:len(search.events)-1]
	}()

	if eventIndex+1 == len(search.records) {
		search.considerSolution()
		return
	}
	if token == 1 {
		search.searchAfterNew(offset+1, eventIndex+1)
		return
	}
	search.parseAt(cursor, eventIndex+1)
}

func (search *projectOuterBoneSearch) considerSolution() {
	search.solutions++
	resolution := resolveProjectBoneEventReferences(
		search.records,
		search.directCount,
		search.events,
		search.evidence,
	)
	if betterProjectBoneReferenceResolution(resolution, search.best) {
		search.best = resolution
	}
	if resolution.complete ||
		search.solutions >= projectBoneOuterSolutionLimit {
		search.stopped = true
	}
}

func resolveProjectBoneEventReferences(
	records []ProjectBoneRecord,
	directCount int,
	events []projectOuterBoneEvent,
	evidence map[int]struct{},
) projectBoneReferenceResolution {
	resolution := projectBoneReferenceResolution{
		references: make([]int, len(records)),
		events:     append([]projectOuterBoneEvent(nil), events...),
	}
	for index := 0; index < directCount; index++ {
		resolution.references[index] = projectFirstWireReference + index
	}

	outerNew := make(map[int]struct{}, directCount+len(events))
	for index := 0; index < directCount; index++ {
		outerNew[index] = struct{}{}
	}
	explicitReferences := make([]int, 0, len(events))
	for _, event := range events {
		if event.RecordIndex >= 0 {
			outerNew[event.RecordIndex] = struct{}{}
			continue
		}
		explicitReferences = append(explicitReferences, event.Reference)
	}
	sort.Ints(explicitReferences)

	nestedRecords := make([]int, 0, len(explicitReferences))
	laterNewRecords := make([]int, 0)
	for index := directCount; index < len(records); index++ {
		if _, exists := outerNew[index]; exists {
			laterNewRecords = append(laterNewRecords, index)
		} else {
			nestedRecords = append(nestedRecords, index)
		}
	}
	if len(nestedRecords) != len(explicitReferences) {
		return resolution
	}
	for index, recordIndex := range nestedRecords {
		resolution.references[recordIndex] = explicitReferences[index]
	}

	assigned := make(map[int]struct{}, len(records))
	for _, reference := range resolution.references {
		if reference > 0 {
			assigned[reference] = struct{}{}
		}
	}
	unassignedEvidence := make([]int, 0, len(laterNewRecords))
	for reference := range evidence {
		if reference < projectBoneMinimumWireReference {
			continue
		}
		if _, exists := assigned[reference]; !exists {
			unassignedEvidence = append(unassignedEvidence, reference)
		}
	}
	sort.Ints(unassignedEvidence)
	if len(unassignedEvidence) == len(laterNewRecords) {
		for index, recordIndex := range laterNewRecords {
			reference := unassignedEvidence[index]
			resolution.references[recordIndex] = reference
			assigned[reference] = struct{}{}
		}
	}

	resolution.laterNew = len(laterNewRecords)
	last := 0
	resolution.complete = true
	for _, reference := range resolution.references {
		if reference <= last {
			resolution.complete = false
			continue
		}
		last = reference
		resolution.resolved++
	}
	if !projectBoneReferencesCoverEvidence(resolution.references, evidence) {
		resolution.complete = false
	}
	return resolution
}

func projectBoneReferencesCoverEvidence(
	references []int,
	evidence map[int]struct{},
) bool {
	assigned := make(map[int]struct{}, len(references))
	for _, reference := range references {
		if reference >= projectBoneMinimumWireReference {
			assigned[reference] = struct{}{}
		}
	}
	for reference := range evidence {
		if reference < projectBoneMinimumWireReference {
			continue
		}
		if _, exists := assigned[reference]; !exists {
			return false
		}
	}
	return true
}

func betterProjectBoneReferenceResolution(
	candidate projectBoneReferenceResolution,
	current projectBoneReferenceResolution,
) bool {
	if candidate.references == nil {
		return false
	}
	if current.references == nil {
		return true
	}
	if candidate.complete != current.complete {
		return candidate.complete
	}
	if candidate.resolved != current.resolved {
		return candidate.resolved > current.resolved
	}
	if candidate.laterNew != current.laterNew {
		return candidate.laterNew > current.laterNew
	}
	for index := 0; index < len(candidate.events) &&
		index < len(current.events); index++ {
		if candidate.events[index].Offset != current.events[index].Offset {
			return candidate.events[index].Offset < current.events[index].Offset
		}
	}
	return len(candidate.events) < len(current.events)
}

func collectProjectBoneReferenceEvidence(
	payload []byte,
	records []ProjectBoneRecord,
) map[int]struct{} {
	evidence := make(map[int]struct{}, len(records))
	for _, record := range records {
		if record.ParentToken > 1 {
			evidence[record.ParentToken] = struct{}{}
		}
	}
	collectProjectSlotBoneReferences(payload, evidence)
	if animations, err := DiscoverProjectAnimations(payload); err == nil {
		for _, animation := range animations.Records {
			groups := discoverProjectBoneTimelineGroups(
				payload,
				animation.Offset,
				animation.EndOffset,
			)
			for _, group := range groups {
				evidence[group.BoneReference] = struct{}{}
			}
		}
	}
	return evidence
}

func collectProjectSlotBoneReferences(
	payload []byte,
	evidence map[int]struct{},
) {
	for offset := 0; offset+len(projectSlotRecordPrefix) < len(payload); {
		relative := bytes.Index(payload[offset:], projectSlotRecordPrefix)
		if relative < 0 {
			return
		}
		recordOffset := offset + relative
		_, afterName, ok := decodeProjectASCII(
			payload,
			recordOffset+len(projectSlotRecordPrefix),
		)
		if ok && afterName < len(payload) && payload[afterName] == 0x02 {
			reference, _, decoded := readPositiveVarint(payload, afterName+1)
			if decoded && reference > 1 {
				evidence[reference] = struct{}{}
			}
		}
		offset = recordOffset + 1
	}
}
