package spineparser

import (
	"bytes"
	"testing"
)

func TestDeleteLastProjectAnimation(t *testing.T) {
	payload := projectAnimationPayloadForDeleteTest(2)
	original := append([]byte(nil), payload...)
	document := &ProjectDocument{Payload: payload}

	deleted, report, err := DeleteLastProjectAnimation(document, "walk")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(document.Payload, original) {
		t.Fatal("input document was mutated")
	}
	if report.Animation != "walk" ||
		report.PreviousCount != 2 ||
		report.Count != 1 ||
		report.RegionStart <= report.CountOffset ||
		report.RegionEnd != len(original) ||
		report.BytesRemoved != len(original)-len(deleted.Payload) {
		t.Fatalf("report = %#v", report)
	}
	directory, err := DiscoverProjectAnimations(deleted.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if directory.Count != 1 ||
		len(directory.Records) != 1 ||
		directory.Records[0].Name != "idle" ||
		directory.Records[0].EndOffset != len(deleted.Payload) {
		t.Fatalf("directory = %#v", directory)
	}

	expected := projectAnimationPayloadForDeleteTest(1)
	if !bytes.Equal(deleted.Payload, expected) {
		t.Fatalf("deleted payload:\n%x\nwant:\n%x", deleted.Payload, expected)
	}
}

func TestDeleteLastProjectAnimationRejectsUnsafeDeletion(t *testing.T) {
	tests := []struct {
		name      string
		payload   []byte
		animation string
	}{
		{
			name:      "not terminal",
			payload:   projectAnimationPayloadForDeleteTest(2),
			animation: "idle",
		},
		{
			name:      "missing",
			payload:   projectAnimationPayloadForDeleteTest(2),
			animation: "missing",
		},
		{
			name:      "empty name",
			payload:   projectAnimationPayloadForDeleteTest(2),
			animation: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := append([]byte(nil), test.payload...)
			document := &ProjectDocument{Payload: test.payload}
			if _, _, err := DeleteLastProjectAnimation(
				document,
				test.animation,
			); err == nil {
				t.Fatal("expected deletion error")
			}
			if !bytes.Equal(document.Payload, original) {
				t.Fatal("failed deletion mutated input")
			}
		})
	}
}

func TestDeleteOnlyProjectAnimationLeavesEmptyMap(t *testing.T) {
	payload := projectAnimationPayloadForDeleteTest(1)
	deleted, report, err := DeleteLastProjectAnimation(
		&ProjectDocument{Payload: payload},
		"idle",
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.PreviousCount != 1 || report.Count != 0 {
		t.Fatalf("report = %#v", report)
	}
	directory, err := DiscoverProjectAnimations(deleted.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if directory.Count != 0 || len(directory.Records) != 0 {
		t.Fatalf("directory = %#v", directory)
	}
	if _, _, err := DeleteLastProjectAnimation(deleted, "idle"); err == nil {
		t.Fatal("expected empty animation map rejection")
	}
}

func TestDeleteLastProjectAnimationAllowsDuplicateLeafName(t *testing.T) {
	payload := append([]byte{}, modernAnimationHeaderPrefix...)
	payload = append(payload, 0x02)
	payload = append(payload, modernAnimationHeaderSuffix...)
	payload = append(payload, 0x09)
	payload = append(payload, modernAnimationHeaderTail...)
	for range 2 {
		payload = append(payload, kryoASCIIForTest("hooray")...)
		payload = append(payload, modernAnimationValuePrefix...)
		payload = append(payload, 0x00)
	}
	deleted, _, err := DeleteLastProjectAnimation(
		&ProjectDocument{Payload: payload},
		"hooray",
	)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := DiscoverProjectAnimations(deleted.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if directory.Count != 1 ||
		len(directory.Records) != 1 ||
		directory.Records[0].Name != "hooray" {
		t.Fatalf("directory = %#v", directory)
	}
}

func TestDeleteLastProjectAnimationRewritesMultiByteCount(t *testing.T) {
	payload := projectAnimationPayloadForDeleteTest(128)
	document := &ProjectDocument{Payload: payload}
	deleted, report, err := DeleteLastProjectAnimation(document, "animation-127")
	if err != nil {
		t.Fatal(err)
	}
	if report.PreviousCount != 128 || report.Count != 127 {
		t.Fatalf("report = %#v", report)
	}
	if report.BytesRemoved <= len(kryoASCIIForTest("animation-127")) {
		t.Fatalf("bytesRemoved = %d", report.BytesRemoved)
	}
	directory, err := DiscoverProjectAnimations(deleted.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if directory.Count != 127 ||
		directory.Records[len(directory.Records)-1].Name != "animation-126" {
		t.Fatalf("directory = %#v", directory)
	}
}

func projectAnimationPayloadForDeleteTest(count int) []byte {
	payload := append([]byte{}, modernAnimationHeaderPrefix...)
	payload = appendPositiveVarint(payload, count)
	payload = append(payload, modernAnimationHeaderSuffix...)
	payload = append(payload, 0x09)
	payload = append(payload, modernAnimationHeaderTail...)
	for index := 0; index < count; index++ {
		name := "idle"
		if count == 2 && index == 1 {
			name = "walk"
		} else if count > 2 {
			name = "animation-" + projectTestDecimal(index)
		}
		payload = append(payload, kryoASCIIForTest(name)...)
		payload = append(payload, modernAnimationValuePrefix...)
		payload = append(payload, byte(index), 0x55, 0xaa)
	}
	return payload
}

func projectTestDecimal(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	cursor := len(digits)
	for value > 0 {
		cursor--
		digits[cursor] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[cursor:])
}
