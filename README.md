# spine-file-parser

Go library for inspecting Spine files and exporting complete Spine
Professional project data.

- zero third-party Go dependencies;
- inspect raw-DEFLATE Spine Editor `.spine` projects;
- extract editor version and diagnostic strings;
- inspect current and legacy exported `.skel` headers;
- parse exported Spine JSON;
- invoke the official Spine CLI for complete bones, slots, constraints, skins,
  meshes, events, and animations;
- keep JSON, decoded binary, strings, metadata, and CLI logs in a unique
  temporary directory for manual troubleshooting.

> `.spine` is Spine Editor's private, version-dependent project format. Its
> semantic schema is not public. Pure Go APIs inspect the stable compression
> envelope and metadata. Complete semantic data uses Spine's supported CLI
> export path.

## Install

```bash
go get github.com/neko233-com/spine-file-parser
```

```go
import spineparser "github.com/neko233-com/spine-file-parser"
```

## Inspect a `.spine` project

```go
source, err := os.ReadFile("character.spine")
if err != nil {
	log.Fatal(err)
}

inspection, err := spineparser.InspectProject(
	source,
	spineparser.InspectOptions{},
)
if err != nil {
	log.Fatal(err)
}

fmt.Println(inspection.SpineVersion)
fmt.Println(inspection.Strings)
```

## Inspect a file and keep diagnostics

```go
result, err := spineparser.InspectFile(
	"character.spine",
	spineparser.InspectFileOptions{},
)
if err != nil {
	log.Fatal(err)
}

fmt.Println(result.OutputDirectory)
fmt.Println(result.Artifacts)
```

Default layout:

```text
spine-file-parser-<random>/
└─ diagnostics/
   ├─ character.inspection.json
   ├─ character.strings.txt
   └─ character.decoded.bin
```

Use `OutputDirectory` for a known location. Set `OmitDecodedBinary` to avoid
writing the decompressed private stream.

## Export and parse a Spine Professional project

Requires a locally installed and licensed Spine Editor. This library does not
bundle Spine, Spine Runtimes, or a license.

```go
result, err := spineparser.ExportProject(
	context.Background(),
	"character.spine",
	spineparser.ExportOptions{
		Executable:    "D:/IDE/Spine/Spine.com",
		EditorVersion: "4.3.xx",
	},
)
if err != nil {
	log.Fatal(err)
}

fmt.Println(result.OutputDirectory)
for _, document := range result.Documents {
	fmt.Println(document.FileName)
	fmt.Println(len(document.Data.Bones))
	fmt.Println(document.Data.Animations)
}
```

Output is intentionally kept:

```text
spine-file-parser-<random>/
├─ character.json
└─ diagnostics/
   ├─ character.inspection.json
   ├─ character.strings.txt
   ├─ character.decoded.bin
   └─ character.spine-cli.log
```

Failed CLI exports also keep diagnostics and include the directory in the
returned error. Set `SPINE_EXECUTABLE` instead of passing `Executable` on every
call.

## Resource limits

Project decompression is limited to 256 MiB by default:

```go
inspection, err := spineparser.InspectProject(
	source,
	spineparser.InspectOptions{
		MaxUncompressedBytes: 512 * 1024 * 1024,
		MaxStrings:           20_000,
	},
)
```

## TypeScript compatibility package

Existing TypeScript API remains available:

```bash
npm install spine-file-parser
```

New development targets the Go module.

## License

MIT. Spine is a trademark of Esoteric Software LLC. Spine Editor and Spine
Runtimes have their own licenses.
