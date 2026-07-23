package spineparser

import "testing"

func TestDiscoverProjectBoneReferencesDirect(t *testing.T) {
	payload := []byte{0x55}
	payload = append(payload, projectBoneTablePrefix...)
	payload = append(payload, 0x03, 0x0c)
	payload = append(payload, projectBoneRecordForReferenceTest("root", 0)...)
	payload = append(payload, 0x7e, 0x00, 0x0c)
	payload = append(payload, projectBoneRecordForReferenceTest("body", 4)...)
	payload = append(payload, 0x7e, 0x00, 0x0c)
	payload = append(payload, projectBoneRecordForReferenceTest("hand", 5)...)

	directory, err := DiscoverProjectBones(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !directory.ReferencesComplete {
		t.Fatalf("references incomplete: %#v", directory.Records)
	}
	for index, record := range directory.Records {
		want := projectFirstWireReference + index
		if record.WireReference != want {
			t.Fatalf("%s reference = %d, want %d", record.Name, record.WireReference, want)
		}
	}
	if reference, ok := directory.WireReferenceByName("body"); !ok || reference != 5 {
		t.Fatalf("body reference = %d, %v", reference, ok)
	}
	if name, ok := directory.BoneNameByWireReference(6); !ok || name != "hand" {
		t.Fatalf("reference 6 = %q, %v", name, ok)
	}
}

func TestDiscoverProjectBoneReferencesNestedAndLaterNew(t *testing.T) {
	payload := []byte{0x55}
	payload = append(payload, projectBoneTablePrefix...)
	payload = append(payload, 0x03, 0x0c)
	payload = append(payload, projectBoneRecordForReferenceTest("root", 0)...)
	payload = append(payload, 0x55)
	payload = append(payload, projectBoneRecordForReferenceTest("nested", 4)...)
	payload = append(payload, 0x7e, 0x00, 0x0c, 0x06, 0x0c)
	payload = append(payload, projectBoneRecordForReferenceTest("late", 4)...)
	payload = append(payload, 0x7e, 0x00)
	payload = append(payload, projectSlotRecordPrefix...)
	payload = append(payload, kryoASCIIForTest("late-slot")...)
	payload = append(payload, 0x02, 0x09)

	directory, err := DiscoverProjectBones(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !directory.ReferencesComplete {
		t.Fatalf("references incomplete: %#v", directory.Records)
	}
	want := map[string]int{"root": 4, "nested": 6, "late": 9}
	for _, record := range directory.Records {
		if record.WireReference != want[record.Name] {
			t.Fatalf(
				"%s reference = %d, want %d",
				record.Name,
				record.WireReference,
				want[record.Name],
			)
		}
	}
}

func TestProjectOuterReferenceTailStartsAfterDirectPrefix(t *testing.T) {
	payload := make([]byte, 40)
	payload[10], payload[11] = 0x0c, 0x06
	payload[30], payload[31] = 0x0c, 0x08

	events, ok := projectOuterReferenceTail(
		payload,
		0x0c,
		[]int{10, 30},
		20,
		1,
		2,
	)
	if !ok || len(events) != 1 ||
		events[0].Offset != 30 || events[0].Reference != 8 {
		t.Fatalf("events = %#v, ok = %v", events, ok)
	}
}

func TestProjectBoneReferenceResolutionRequiresAllEvidence(t *testing.T) {
	records := []ProjectBoneRecord{{Name: "root"}, {Name: "nested"}}
	events := []projectOuterBoneEvent{{
		Offset:      10,
		RecordIndex: -1,
		Reference:   6,
	}}
	resolution := resolveProjectBoneEventReferences(
		records,
		1,
		events,
		map[int]struct{}{7: {}},
	)
	if resolution.complete {
		t.Fatalf("coincidental reference run was accepted: %#v", resolution)
	}
}

func TestDirectProjectBoneReferencesRejectContradictingEvidence(t *testing.T) {
	payload := []byte{0x55}
	payload = append(payload, projectBoneTablePrefix...)
	payload = append(payload, 0x03, 0x0c)
	payload = append(payload, projectBoneRecordForReferenceTest("root", 0)...)
	payload = append(payload, 0x7e, 0x00, 0x0c)
	payload = append(payload, projectBoneRecordForReferenceTest("body", 4)...)
	payload = append(payload, 0x7e, 0x00, 0x0c)
	payload = append(payload, projectBoneRecordForReferenceTest("hand", 5)...)
	payload = append(payload, 0x7e, 0x00)
	payload = append(payload, projectSlotRecordPrefix...)
	payload = append(payload, kryoASCIIForTest("contradiction")...)
	payload = append(payload, 0x02, 0x63)

	directory, err := DiscoverProjectBones(payload)
	if err != nil {
		t.Fatal(err)
	}
	if directory.ReferencesComplete {
		t.Fatalf("contradicting evidence was accepted: %#v", directory.Records)
	}
	if directory.Records[0].WireReference != projectFirstWireReference ||
		directory.Records[1].WireReference != 0 ||
		directory.Records[2].WireReference != 0 {
		t.Fatalf("unsafe direct references were exposed: %#v", directory.Records)
	}
}

func projectBoneRecordForReferenceTest(name string, parent int) []byte {
	record := []byte{0x01, 0x1d, 0x00, 0x03, 0x01, 0x01}
	record = append(record, kryoASCIIForTest(name)...)
	record = append(record, 0x02, 0x01, 0x03, byte(parent))
	return record
}
