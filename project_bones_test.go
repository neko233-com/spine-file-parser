package spineparser

import "testing"

func TestDiscoverProjectBones(t *testing.T) {
	payload := []byte{0x55}
	payload = append(payload, projectBoneTablePrefix...)
	payload = append(payload, 0x03, 0x0c, 0x01, 0x1d, 0x00, 0x03, 0x01)
	payload = append(payload, 0x01)
	payload = append(payload, kryoASCIIForTest("root")...)
	payload = append(payload, 0x02, 0x01, 0x03, 0x00)
	payload = append(payload, 0x01, 0x1d, 0x00, 0x03, 0x01, 0x01)
	payload = append(payload, kryoASCIIForTest("body")...)
	payload = append(payload, 0x02, 0x00, 0x03, 0x04)
	payload = append(payload, 0x01, 0x11, 0x00, 0x03, 0x01, 0x01)
	payload = append(payload, kryoASCIIForTest("hand")...)
	payload = append(payload, 0x02)
	payload = append(payload, 0x01, 0x1d, 0x00, 0x03, 0x01, 0x56)
	payload = append(payload, 0x02, 0x01, 0x03, 0x05)

	directory, err := DiscoverProjectBones(payload)
	if err != nil {
		t.Fatal(err)
	}
	if directory.Count != 3 || directory.ClassID != 0x1d {
		t.Fatalf("directory = %#v", directory)
	}
	if directory.Records[0].Name != "root" ||
		directory.Records[0].ParentToken != 0 ||
		directory.Records[1].Name != "body" ||
		directory.Records[1].ParentToken != 4 ||
		directory.Records[2].Name != "hand" ||
		directory.Records[2].ParentToken != 5 ||
		directory.Records[2].NameEncoding != "wrapper-reference" {
		t.Fatalf("records = %#v", directory.Records)
	}
}

func TestDiscoverProjectBonesRejectsInvalidPayload(t *testing.T) {
	if _, err := DiscoverProjectBones([]byte("not a project")); err == nil {
		t.Fatal("expected unsupported bone-table error")
	}
}
