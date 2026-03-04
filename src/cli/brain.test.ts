import { describe, expect, test, beforeEach, afterEach } from "bun:test";
import { spawnSync } from "child_process";
import { dirname, join } from "path";
import { existsSync, mkdirSync, rmSync } from "fs";
import { tmpdir } from "os";

const repoRoot = dirname(dirname(import.meta.dir));

function runBrainCli(args: string[], env?: Record<string, string>) {
	return spawnSync(process.execPath, ["run", "src/cli/brain.ts", ...args], {
		cwd: repoRoot,
		encoding: "utf-8",
		env: { ...process.env, ...env },
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

describe("brain init command", () => {
	let tempDir: string;

	beforeEach(() => {
		tempDir = join(tmpdir(), `brain-init-test-${Date.now()}-${Math.random().toString(36).slice(2)}`);
		mkdirSync(tempDir, { recursive: true });
	});

	afterEach(() => {
		if (existsSync(tempDir)) {
			rmSync(tempDir, { recursive: true, force: true });
		}
	});

	test("creates brain.db during init", () => {
		const result = runBrainCli(["init"], { BRAIN_DIR: tempDir });
		const output = `${result.stdout}${result.stderr}`;

		// Should succeed
		expect(result.status).toBe(0);

		// brain.db should be created
		expect(existsSync(join(tempDir, "brain.db"))).toBe(true);

		// Output should mention brain.db initialization
		expect(output).toContain("brain.db");
	});

	test("does not call zk CLI during init", () => {
		const result = runBrainCli(["init"], { BRAIN_DIR: tempDir });
		const output = `${result.stdout}${result.stderr}`;

		// Should succeed even without zk CLI
		expect(result.status).toBe(0);

		// Should NOT mention "zk init" or "zk CLI not found"
		expect(output).not.toContain("zk CLI not found");
		expect(output).not.toContain("zk init");
	});

	test("does not create .zk directory during init (ZK dependency removed)", () => {
		const result = runBrainCli(["init"], { BRAIN_DIR: tempDir });

		expect(result.status).toBe(0);
		expect(existsSync(join(tempDir, ".zk"))).toBe(false);
	});

	test("dry-run does not create brain.db", () => {
		const result = runBrainCli(["init", "--dry-run"], { BRAIN_DIR: tempDir });

		expect(result.status).toBe(0);
		// brain.db should NOT be created in dry-run
		expect(existsSync(join(tempDir, "brain.db"))).toBe(false);
	});
});
