import { afterEach, describe, expect, test } from "bun:test";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from "fs";
import { tmpdir } from "os";
import { join } from "path";
import { installPlugin } from "./installer";

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
  test("opencode install overwrites existing files without --force and without backups", async () => {
    const home = createTestHome();
    const pluginDir = join(home, ".config/opencode/plugin");
    const pluginPath = join(pluginDir, "brain.ts");
    const skillDir = join(home, ".config/opencode/skill/do-work-queue");
    const skillPath = join(skillDir, "SKILL.md");

    mkdirSync(pluginDir, { recursive: true });
    mkdirSync(skillDir, { recursive: true });
    writeFileSync(pluginPath, "old plugin file");
    writeFileSync(skillPath, "old skill file");

    const result = await installPlugin({ target: "opencode" });

    expect(result.success).toBe(true);
    expect(result.message).toContain("Successfully installed OpenCode components");
    expect(result.installedFiles?.every((file) => file.backupPath === undefined)).toBe(true);

    const pluginContent = await Bun.file(pluginPath).text();
    const skillContent = await Bun.file(skillPath).text();
    expect(pluginContent).not.toBe("old plugin file");
    expect(skillContent).not.toBe("old skill file");
    expect(pluginContent).toContain("To update: brain install opencode");
    expect(pluginContent).not.toContain("To update: brain install opencode --force");

    expect(readdirSync(pluginDir).some((name) => name.startsWith("brain.ts.backup-"))).toBe(false);
    expect(readdirSync(skillDir).some((name) => name.startsWith("SKILL.md.backup-"))).toBe(false);
  });

});
