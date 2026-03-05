import { describe, expect, test } from "bun:test";
import { spawnSync } from "child_process";
import { dirname } from "path";

const repoRoot = dirname(dirname(import.meta.dir));

function runBrainCli(args: string[]) {
	return spawnSync(process.execPath, ["run", "src/cli/brain.ts", ...args], {
		cwd: repoRoot,
		encoding: "utf-8",
	});
}

describe("brain CLI command surface", () => {
	test("help documents destructive default install behavior", () => {
		const result = runBrainCli(["--help"]);

		expect(result.status).toBe(0);
		expect(result.stdout).toContain(
			"brain install opencode              Install/replace OpenCode (destructive by default)",
		);
		expect(result.stdout).toContain("brain install opencode");
		expect(result.stdout).toContain(
			"brain install opencode --force      Explicit destructive replacement",
		);
		expect(result.stdout).not.toContain("brain clean-up");
		expect(result.stdout).not.toContain("brain cleanup");
		expect(result.stdout).not.toContain("backup command");
	});

	test("removed backup lifecycle commands are rejected", () => {
		const removedCommands = ["clean-up", "cleanup", "backups", "restore", "clean-backups"];

		for (const removedCommand of removedCommands) {
			const result = runBrainCli([removedCommand]);
			const output = `${result.stdout}${result.stderr}`;
			expect(result.status).toBe(1);
			expect(output).toContain(`Unknown command: ${removedCommand}`);
		}
	});

	test("unknown command is rejected", () => {
		const result = runBrainCli(["not-a-real-command"]);
		const output = `${result.stdout}${result.stderr}`;

		expect(result.status).toBe(1);
		expect(output).toContain("Unknown command: not-a-real-command");
	});
});
