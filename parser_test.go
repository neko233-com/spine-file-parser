package spineparser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("test", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInspectOfficialProProject(t *testing.T) {
	source := fixture(t, "coin-pro.spine")
	result, err := InspectProject(source, InspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.SpineVersion != "4.0.07" {
		t.Fatalf("version = %q", result.SpineVersion)
	}
	if result.UncompressedBytes != 11399 {
		t.Fatalf("uncompressed bytes = %d", result.UncompressedBytes)
	}
	if Detect(source) != FileProject {
		t.Fatalf("kind = %q", Detect(source))
	}
	if !contains(result.Strings, "coin-front-shine-logo") {
		t.Fatal("expected project string not found")
	}
}

func TestInspectOfficialSkeletonBinary(t *testing.T) {
	source := fixture(t, "coin-pro.skel")
	result, err := InspectSkeletonBinary(source)
	if err != nil {
		t.Fatal(err)
	}
	if result.SpineVersion != "4.2.22" {
		t.Fatalf("version = %q", result.SpineVersion)
	}
	if result.Hash != "7caafe7dee2b2849" {
		t.Fatalf("hash = %q", result.Hash)
	}
	if result.ReferenceScale == nil || *result.ReferenceScale != 100 {
		t.Fatalf("reference scale = %v", result.ReferenceScale)
	}
}

func TestInspectLimit(t *testing.T) {
	_, err := InspectProject(fixture(t, "coin-pro.spine"), InspectOptions{
		MaxUncompressedBytes: 100,
	})
	var parseErr *ParseError
	if !errors.As(err, &parseErr) || parseErr.Code != ErrLimitExceeded {
		t.Fatalf("error = %#v", err)
	}
}

func TestInspectFileKeepsDiagnostics(t *testing.T) {
	result, err := InspectFile(
		filepath.Join("test", "fixtures", "coin-pro.spine"),
		InspectFileOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(result.OutputDirectory)

	content, err := os.ReadFile(result.Artifacts.StringsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "coin-front-shine-logo") {
		t.Fatal("diagnostic strings missing expected value")
	}
	info, err := os.Stat(result.Artifacts.DecodedBinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 11399 {
		t.Fatalf("decoded size = %d", info.Size())
	}
}

func TestExportFailureKeepsDiagnostics(t *testing.T) {
	output := t.TempDir()
	_, err := ExportProject(
		context.Background(),
		filepath.Join("test", "fixtures", "coin-pro.spine"),
		ExportOptions{
			InspectFileOptions: InspectFileOptions{OutputDirectory: output},
			Executable:         "__missing_spine_executable__",
		},
	)
	if err == nil || !strings.Contains(err.Error(), "diagnostics kept at") {
		t.Fatalf("error = %v", err)
	}
	content, readErr := os.ReadFile(filepath.Join(
		output,
		"diagnostics",
		"coin-pro.spine-cli.log",
	))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(content), "# error") {
		t.Fatal("CLI failure log missing error")
	}
}

func TestIntegrationExportOfficialProProject(t *testing.T) {
	if os.Getenv("SPINE_INTEGRATION") != "1" {
		t.Skip("set SPINE_INTEGRATION=1 to use the locally licensed Spine CLI")
	}
	result, err := ExportProject(
		context.Background(),
		filepath.Join("test", "fixtures", "coin-pro.spine"),
		ExportOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 || len(result.Documents[0].Data.Bones) != 7 {
		t.Fatalf("documents = %#v", result.Documents)
	}
	t.Logf("diagnostics kept at %s", result.OutputDirectory)
}

func TestParseJSON(t *testing.T) {
	result, err := ParseJSON([]byte(`{
		"skeleton":{"spine":"4.2.0"},
		"bones":[{"name":"root"}],
		"animations":{"idle":{}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Skeleton == nil || result.Skeleton.Spine != "4.2.0" {
		t.Fatalf("skeleton = %#v", result.Skeleton)
	}
	if len(result.Bones) != 1 || result.Bones[0].Name != "root" {
		t.Fatalf("bones = %#v", result.Bones)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
