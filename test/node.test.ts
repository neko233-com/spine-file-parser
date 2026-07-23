import { mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  exportSpineProject,
  inspectSpineProjectFile
} from "../src/node";

const fixture = fileURLToPath(
  new URL("./fixtures/coin-pro.spine", import.meta.url)
);

describe("Node diagnostics", () => {
  it("keeps inspection output in a unique temporary directory", async () => {
    const result = await inspectSpineProjectFile(fixture);

    try {
      expect(result.outputDirectory.startsWith(tmpdir())).toBe(true);
      expect(result.inspection.spineVersion).toBe("4.0.07");
      expect(
        JSON.parse(
          await readFile(result.artifacts.inspectionPath, "utf8")
        ).inspection.spineVersion
      ).toBe("4.0.07");
      expect(
        await readFile(result.artifacts.stringsPath, "utf8")
      ).toContain("coin-front-shine-logo");
      expect(
        (await stat(result.artifacts.decodedBinaryPath!)).size
      ).toBe(11399);
    } finally {
      await rm(result.outputDirectory, { recursive: true, force: true });
    }
  });

  it("keeps diagnostics when the Spine CLI fails", async () => {
    const outputDirectory = await mkdtemp(
      join(tmpdir(), "spine-file-parser-failure-")
    );

    try {
      await expect(
        exportSpineProject(fixture, {
          executable: "__missing_spine_executable__",
          outputDirectory
        })
      ).rejects.toThrow(`Diagnostic output kept at ${outputDirectory}`);

      const log = await readFile(
        join(
          outputDirectory,
          "diagnostics",
          "coin-pro.spine-cli.log"
        ),
        "utf8"
      );
      expect(log).toContain("# error");
    } finally {
      await rm(outputDirectory, { recursive: true, force: true });
    }
  });
});
