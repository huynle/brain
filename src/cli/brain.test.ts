import { describe, expect, test } from "bun:test";
import { spawnSync } from "child_process";
import { dirname, join } from "path";

const repoRoot = dirname(dirname(import.meta.dir));

function runBrainCli(args: string[]) {
  return spawnSync(process.execPath, ["run", "src/cli/brain.ts", ...args], {
    cwd: repoRoot,
    encoding: "utf-8",
  });
}

describe("brain CLI command surface", () => {
  test("help excludes backup lifecycle commands and force reinstall guidance", () => {
    const result = runBrainCli(["--help"]);

    expect(result.status).toBe(0);
    expect(result.stdout).not.toContain("backups <target>");
    expect(result.stdout).not.toContain("restore <target>");
    expect(result.stdout).not.toContain("clean-backups");
    expect(result.stdout).toContain("brain install opencode");
    expect(result.stdout).not.toContain("brain install opencode --force");
    expect(result.stdout).not.toContain("Usage: brain install <target> [--force]");
    expect(result.stdout).not.toContain("brain clean-up");
  });

  test("backups is not a recognized command", () => {
    const result = runBrainCli(["backups", "opencode"]);
    const output = `${result.stdout}${result.stderr}`;

    expect(result.status).toBe(1);
    expect(output).toContain("Unknown command: backups");
  });

  test("restore is not a recognized command", () => {
    const result = runBrainCli(["restore", "opencode"]);
    const output = `${result.stdout}${result.stderr}`;

    expect(result.status).toBe(1);
    expect(output).toContain("Unknown command: restore");
  });

  test("clean-backups is not a recognized command", () => {
    const result = runBrainCli(["clean-backups", "opencode"]);
    const output = `${result.stdout}${result.stderr}`;

    expect(result.status).toBe(1);
    expect(output).toContain("Unknown command: clean-backups");
  });

  test("clean-up is not a recognized command", () => {
    const result = runBrainCli(["clean-up", "opencode"]);
    const output = `${result.stdout}${result.stderr}`;

    expect(result.status).toBe(1);
    expect(output).toContain("Unknown command: clean-up");
  });
});
