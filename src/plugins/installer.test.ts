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
		const commandPath = join(home, ".config/opencode/command/do.md");
		const checkoutSkillPath = join(
			home,
			".config/opencode/skill/feature-checkout/SKILL.md",
		);
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
		const commandContent = await Bun.file(commandPath).text();
		const checkoutSkillContent = await Bun.file(checkoutSkillPath).text();
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

		// Checkout direct_prompt contract: must load skill and validate intent
		expect(commandContent).toContain(
			"Load the feature-checkout skill and process the checkout task at brain path:",
		);
		expect(commandContent).toContain(
			"Validate implementation coverage against dependency tasks' user_original_request intent. Start now.",
		);
		expect(checkoutSkillContent).toContain(
			"Load the feature-checkout skill and process the checkout task at brain path:",
		);
		expect(checkoutSkillContent).toContain(
			"Validate implementation coverage against dependency tasks' user_original_request intent. Start now.",
		);

		// Generated metadata contract in auto-generated task examples
		expect(commandContent).toContain("generated: true");
		expect(commandContent).toContain('generated_kind: "feature_checkout"');
		expect(commandContent).toContain(
			'generated_key: "feature-checkout:${FEATURE_ID}:round-1"',
		);
		expect(commandContent).toContain('generated_by: "feature-checkout"');
		expect(checkoutSkillContent).toContain('generated_kind: "gap_task"');
		expect(checkoutSkillContent).toContain(
			'generated_key: "feature-checkout:gap:<feature_id>:criterion-<N>"',
		);
		expect(checkoutSkillContent).toContain('generated_kind: "feature_checkout"');
		expect(checkoutSkillContent).toContain(
			'generated_key: "feature-checkout:<feature_id>:round-<N+1>"',
		);
		expect(checkoutSkillContent).toContain('generated_by: "feature-checkout"');

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
