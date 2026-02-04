/**
 * Brain Doctor - Types
 *
 * Type definitions for the brain doctor diagnostic tool.
 */

export type CheckStatus = "pass" | "fail" | "warn" | "skip";

export interface Check {
  name: string;
  status: CheckStatus;
  message: string;
  fixable: boolean;
  details?: string;
}

export interface DoctorResult {
  brainDir: string;
  timestamp: string;
  checks: Check[];
  summary: {
    passed: number;
    failed: number;
    warnings: number;
    skipped: number;
  };
  healthy: boolean;
}

export interface DoctorOptions {
  fix?: boolean;
  force?: boolean;
  dryRun?: boolean;
  verbose?: boolean;
}
