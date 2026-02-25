import { afterEach, describe, expect, test } from "bun:test";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from "fs";
import { tmpdir } from "os";
import { join } from "path";
import { installPlugin, uninstallPlugin } from "./installer";

const createdHomes: string[] = [];
const originalHome = process.env.HOME;

afterEach(() => {
	if (originalHome === undefined) {
		delete process.env.HOME;
	} else {
		process.env.HOME = originalHome;
	}

	for (const dir of createdHomes.splice(0)) {
		if (existsSync(dir)) {
			rmSync(dir, { recursive: true, force: true });
		}
	}
});

function createTestHome(): string {
	const home = mkdtempSync(join(tmpdir(), "brain-installer-test-"));
	createdHomes.push(home);
	process.env.HOME = home;
	return home;
}

describe("installPlugin", () => {
	test("opencode install overwrites existing files without --force", async () => {
		const home = createTestHome();
		const pluginDir = join(home, ".config/opencode/plugin");
		const pluginPath = join(pluginDir, "brain.ts");
		const skillDir = join(home, ".config/opencode/skill/do-work-queue");
		const skillPath = join(skillDir, "SKILL.md");
		const pluginBackupPath = join(pluginDir, "brain.ts.backup");
		const pluginBakPath = join(pluginDir, "brain.ts.bak");
		const backupDir = join(home, ".config/opencode/backup");

		mkdirSync(pluginDir, { recursive: true });
		mkdirSync(skillDir, { recursive: true });
		writeFileSync(pluginPath, "old plugin file");
		writeFileSync(skillPath, "old skill file");

		const result = await installPlugin({ target: "opencode" });

		expect(result.success).toBe(true);
		expect(result.message).toContain(
			"Successfully installed OpenCode components",
		);

		const pluginContent = await Bun.file(pluginPath).text();
		const skillContent = await Bun.file(skillPath).text();
		expect(pluginContent).not.toBe("old plugin file");
		expect(skillContent).not.toBe("old skill file");
		expect(pluginContent).toContain(
			"This file was installed by: brain install opencode",
		);
		expect(pluginContent).toContain("To update: brain install opencode");
		expect(pluginContent).toContain("To check status: brain doctor");
		expect(pluginContent).toContain(
			"Source: https://github.com/huynle/brain-api",
		);
		expect(existsSync(pluginBackupPath)).toBe(false);
		expect(existsSync(pluginBakPath)).toBe(false);
		expect(existsSync(backupDir)).toBe(false);

		const pluginDirEntries = readdirSync(pluginDir);
		expect(
			pluginDirEntries.some((entry) => entry.startsWith("brain.ts.uninstalled-")),
		).toBe(false);
	});

	test("opencode uninstall removes plugin without creating backups", async () => {
		const home = createTestHome();
		const pluginDir = join(home, ".config/opencode/plugin");
		const pluginPath = join(pluginDir, "brain.ts");
		const pluginBackupPath = join(pluginDir, "brain.ts.backup");
		const pluginBakPath = join(pluginDir, "brain.ts.bak");
		const backupDir = join(home, ".config/opencode/backup");

		mkdirSync(pluginDir, { recursive: true });
		writeFileSync(pluginPath, "installed plugin file");

		const result = await uninstallPlugin("opencode");

		expect(result.success).toBe(true);
		expect(existsSync(pluginPath)).toBe(false);
		expect(existsSync(pluginBackupPath)).toBe(false);
		expect(existsSync(pluginBakPath)).toBe(false);
		expect(existsSync(backupDir)).toBe(false);

		const pluginDirEntries = readdirSync(pluginDir);
		expect(
			pluginDirEntries.some((entry) => entry.startsWith("brain.ts.uninstalled-")),
		).toBe(false);
	});
});
