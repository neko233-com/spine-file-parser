import { spawn } from "node:child_process";
import {
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  writeFile
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, extname, join, parse, resolve } from "node:path";

import {
  decodeSpineProject,
  inspectSpineProject,
  parseSpineJson
} from "./index";
import type {
  InspectProjectOptions,
  ParsedSpineJson,
  SpineProjectInspection
} from "./index";

export * from "./index";

export interface InspectSpineProjectFileOptions extends InspectProjectOptions {
  /**
   * Output directory. A unique OS temporary directory is used by default.
   */
  outputDirectory?: string;
  /**
   * Write the decompressed private project stream for binary inspection.
   * @default true
   */
  writeDecodedBinary?: boolean;
}

export interface SpineProjectDiagnosticArtifacts {
  directory: string;
  inspectionPath: string;
  stringsPath: string;
  decodedBinaryPath?: string;
  cliLogPath?: string;
}

export interface InspectSpineProjectFileResult {
  inspection: SpineProjectInspection;
  outputDirectory: string;
  artifacts: SpineProjectDiagnosticArtifacts;
}

export interface ExportSpineProjectOptions
  extends InspectSpineProjectFileOptions {
  /**
   * Spine CLI executable. Defaults to SPINE_EXECUTABLE, then Spine.com on
   * Windows or Spine elsewhere.
   */
  executable?: string;
  /**
   * Existing Spine export settings JSON. Defaults to Spine's `json` preset.
   */
  exportSettings?: string;
  /**
   * Editor version passed to `--update`, for example `4.2.xx`.
   */
  editorVersion?: string;
  /**
   * Process timeout.
   * @default 120000
   */
  timeoutMs?: number;
}

export interface ExportedSpineDocument {
  fileName: string;
  path: string;
  parsed: ParsedSpineJson;
}

export interface ExportSpineProjectResult {
  inspection: SpineProjectInspection;
  documents: ExportedSpineDocument[];
  outputDirectory: string;
  artifacts: SpineProjectDiagnosticArtifacts;
  stdout: string;
  stderr: string;
}

interface ProcessResult {
  stdout: string;
  stderr: string;
}

function run(
  executable: string,
  args: string[],
  timeoutMs: number
): Promise<ProcessResult> {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(executable, args, {
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"]
    });
    let stdout = "";
    let stderr = "";
    let settled = false;

    const timer = setTimeout(() => {
      child.kill();
      if (!settled) {
        settled = true;
        reject(new Error(`Spine CLI timed out after ${timeoutMs} ms.`));
      }
    }, timeoutMs);

    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk: string) => {
      stderr += chunk;
    });
    child.on("error", (error) => {
      clearTimeout(timer);
      if (!settled) {
        settled = true;
        reject(error);
      }
    });
    child.on("close", (code) => {
      clearTimeout(timer);
      if (settled) return;
      settled = true;
      if (code === 0) {
        resolvePromise({ stdout, stderr });
      } else {
        const output = [stdout, stderr].filter(Boolean).join("\n").trim();
        reject(
          new Error(
            `Spine CLI exited with code ${code ?? "unknown"}` +
              (output ? `:\n${output}` : ".")
          )
        );
      }
    });
  });
}

async function prepareOutputDirectory(
  requestedDirectory?: string
): Promise<string> {
  if (requestedDirectory) {
    const directory = resolve(requestedDirectory);
    await mkdir(directory, { recursive: true });
    return directory;
  }
  return mkdtemp(join(tmpdir(), "spine-file-parser-"));
}

async function writeInspectionArtifacts(
  absoluteProjectPath: string,
  outputDirectory: string,
  inspection: SpineProjectInspection,
  source: Uint8Array,
  options: InspectSpineProjectFileOptions
): Promise<SpineProjectDiagnosticArtifacts> {
  const directory = join(outputDirectory, "diagnostics");
  await mkdir(directory, { recursive: true });
  const stem = parse(absoluteProjectPath).name;
  const inspectionPath = join(directory, `${stem}.inspection.json`);
  const stringsPath = join(directory, `${stem}.strings.txt`);
  const writeDecodedBinary = options.writeDecodedBinary ?? true;
  const decodedBinaryPath = writeDecodedBinary
    ? join(directory, `${stem}.decoded.bin`)
    : undefined;

  await Promise.all([
    writeFile(
      inspectionPath,
      `${JSON.stringify(
        {
          sourcePath: absoluteProjectPath,
          generatedAt: new Date().toISOString(),
          inspection
        },
        null,
        2
      )}\n`,
      "utf8"
    ),
    writeFile(
      stringsPath,
      inspection.strings.length > 0
        ? `${inspection.strings.join("\n")}\n`
        : "",
      "utf8"
    ),
    ...(decodedBinaryPath
      ? [
          writeFile(
            decodedBinaryPath,
            decodeSpineProject(source, {
              ...(options.maxUncompressedBytes === undefined
                ? {}
                : {
                    maxUncompressedBytes:
                      options.maxUncompressedBytes
                  })
            })
          )
        ]
      : [])
  ]);

  return {
    directory,
    inspectionPath,
    stringsPath,
    ...(decodedBinaryPath === undefined ? {} : { decodedBinaryPath })
  };
}

export async function inspectSpineProjectFile(
  projectPath: string,
  options: InspectSpineProjectFileOptions = {}
): Promise<InspectSpineProjectFileResult> {
  const absoluteProjectPath = resolve(projectPath);
  const source = await readFile(absoluteProjectPath);
  const inspection = inspectSpineProject(source, {
    ...(options.maxUncompressedBytes === undefined
      ? {}
      : { maxUncompressedBytes: options.maxUncompressedBytes }),
    ...(options.maxStrings === undefined
      ? {}
      : { maxStrings: options.maxStrings })
  });
  const outputDirectory = await prepareOutputDirectory(
    options.outputDirectory
  );
  const artifacts = await writeInspectionArtifacts(
    absoluteProjectPath,
    outputDirectory,
    inspection,
    source,
    options
  );

  return { inspection, outputDirectory, artifacts };
}

export async function exportSpineProject(
  projectPath: string,
  options: ExportSpineProjectOptions = {}
): Promise<ExportSpineProjectResult> {
  const absoluteProjectPath = resolve(projectPath);
  const inspected = await inspectSpineProjectFile(
    absoluteProjectPath,
    options
  );
  const { inspection, outputDirectory } = inspected;
  const cliLogPath = join(
    inspected.artifacts.directory,
    `${parse(absoluteProjectPath).name}.spine-cli.log`
  );
  const artifacts: SpineProjectDiagnosticArtifacts = {
    ...inspected.artifacts,
    cliLogPath
  };
  const executable =
    options.executable ??
    process.env.SPINE_EXECUTABLE ??
    (process.platform === "win32" ? "Spine.com" : "Spine");
  const timeoutMs = options.timeoutMs ?? 120_000;

  if (!Number.isSafeInteger(timeoutMs) || timeoutMs <= 0) {
    throw new TypeError("timeoutMs must be a positive safe integer.");
  }

  const args = ["--hide-license"];
  if (options.editorVersion) {
    args.push("--update", options.editorVersion);
  }
  args.push(
    "--input",
    absoluteProjectPath,
    "--output",
    outputDirectory,
    "--export",
    options.exportSettings ? resolve(options.exportSettings) : "json"
  );

  try {
    const result = await run(executable, args, timeoutMs);
    await writeFile(
      cliLogPath,
      [
        "# stdout",
        result.stdout.trimEnd(),
        "",
        "# stderr",
        result.stderr.trimEnd(),
        ""
      ].join("\n"),
      "utf8"
    );
    const entries = await readdir(outputDirectory, { withFileTypes: true });
    const jsonFiles = entries
      .filter(
        (entry) =>
          entry.isFile() && extname(entry.name).toLowerCase() === ".json"
      )
      .map((entry) => entry.name)
      .sort();

    if (jsonFiles.length === 0) {
      throw new Error(
        `Spine CLI produced no JSON files for ${basename(projectPath)}.`
      );
    }

    const documents = await Promise.all(
      jsonFiles.map(async (fileName) => {
        const path = join(outputDirectory, fileName);
        return {
          fileName,
          path,
          parsed: parseSpineJson(await readFile(path))
        };
      })
    );

    return {
      inspection,
      documents,
      outputDirectory,
      artifacts,
      stdout: result.stdout,
      stderr: result.stderr
    };
  } catch (error) {
    await writeFile(
      cliLogPath,
      `# error\n${error instanceof Error ? error.stack ?? error.message : String(error)}\n`,
      "utf8"
    );
    throw new Error(
      `Spine export failed. Diagnostic output kept at ${outputDirectory}.`,
      { cause: error }
    );
  }
}
