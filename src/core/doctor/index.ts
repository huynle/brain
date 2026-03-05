/**
 * Brain Doctor - Module Exports
 */

export { DoctorService, createDoctorService } from "./doctor-service";
export type { Check, CheckStatus, DoctorResult, DoctorOptions, VersionCheck } from "./types";
export {
  TOOLS,
  checkToolVersion,
  checkAllVersions,
  getInstalledVersion,
  getLatestVersion,
  compareVersions,
  isOutdated,
  getToolInfo,
} from "./version-checker";
export type { ToolInfo, VersionResult } from "./version-checker";
