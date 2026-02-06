/**
 * System Utilities
 *
 * Utility functions for system resource monitoring.
 *
 * IMPORTANT: Memory calculation on macOS/Linux
 * --------------------------------------------
 * Node's os.freemem() returns only truly "free" memory, not "available" memory.
 * On macOS/Linux, the OS aggressively uses free memory for file caching.
 * This cached memory is immediately reclaimable when apps need it.
 *
 * - macOS: free memory ~3GB (8%) while available is ~18GB (49%)
 * - Linux: same issue - MemFree vs MemAvailable
 *
 * This module uses platform-specific methods to get true available memory:
 * - macOS: vm_stat (free + inactive + purgeable)
 * - Linux: /proc/meminfo MemAvailable
 * - Windows/fallback: os.freemem() (works correctly)
 */

import { freemem, totalmem, platform } from "os";
import { execSync } from "child_process";
import { readFileSync, existsSync } from "fs";

/**
 * Memory provider interface for dependency injection in tests.
 */
export interface MemoryProvider {
  freemem: () => number;
  totalmem: () => number;
  /** Platform-aware available memory (accounts for reclaimable cache) */
  availablemem?: () => number;
}

/**
 * Parse macOS vm_stat output to get available memory.
 * Available = free + inactive + purgeable + speculative
 *
 * @returns Available memory in bytes, or null if parsing fails
 */
function getMacOSAvailableMemory(): number | null {
  try {
    const output = execSync("vm_stat", { encoding: "utf-8", timeout: 5000 });
    const lines = output.split("\n");

    // vm_stat reports pages, first line tells us page size
    // "Mach Virtual Memory Statistics: (page size of 16384 bytes)"
    const pageSizeMatch = lines[0].match(/page size of (\d+) bytes/);
    const pageSize = pageSizeMatch ? parseInt(pageSizeMatch[1], 10) : 16384;

    // Parse page counts
    const getPages = (label: string): number => {
      const line = lines.find((l) => l.includes(label));
      if (!line) return 0;
      const match = line.match(/:\s*(\d+)/);
      return match ? parseInt(match[1], 10) : 0;
    };

    const freePages = getPages("Pages free");
    const inactivePages = getPages("Pages inactive");
    const purgeablePages = getPages("Pages purgeable");
    const speculativePages = getPages("Pages speculative");

    // All of these are reclaimable
    const availablePages =
      freePages + inactivePages + purgeablePages + speculativePages;
    return availablePages * pageSize;
  } catch {
    return null;
  }
}

/**
 * Parse Linux /proc/meminfo to get available memory.
 * Uses MemAvailable if present (kernel 3.14+), otherwise falls back to MemFree.
 *
 * @returns Available memory in bytes, or null if parsing fails
 */
function getLinuxAvailableMemory(): number | null {
  try {
    if (!existsSync("/proc/meminfo")) {
      return null;
    }

    const content = readFileSync("/proc/meminfo", "utf-8");
    const lines = content.split("\n");

    const getValue = (key: string): number | null => {
      const line = lines.find((l) => l.startsWith(key + ":"));
      if (!line) return null;
      // Format: "MemAvailable:   12345678 kB"
      const match = line.match(/:\s*(\d+)\s*kB/);
      return match ? parseInt(match[1], 10) * 1024 : null;
    };

    // Prefer MemAvailable (kernel 3.14+), fall back to MemFree + Buffers + Cached
    const available = getValue("MemAvailable");
    if (available !== null) {
      return available;
    }

    // Fallback for older kernels
    const free = getValue("MemFree");
    const buffers = getValue("Buffers");
    const cached = getValue("Cached");
    if (free !== null) {
      return free + (buffers ?? 0) + (cached ?? 0);
    }

    return null;
  } catch {
    return null;
  }
}

/**
 * Get available memory using platform-specific methods.
 * Falls back to os.freemem() if platform-specific method fails.
 *
 * @returns Available memory in bytes
 */
function getAvailableMemoryBytes(): number {
  const currentPlatform = platform();

  if (currentPlatform === "darwin") {
    const macAvailable = getMacOSAvailableMemory();
    if (macAvailable !== null) {
      return macAvailable;
    }
  } else if (currentPlatform === "linux") {
    const linuxAvailable = getLinuxAvailableMemory();
    if (linuxAvailable !== null) {
      return linuxAvailable;
    }
  }

  // Windows or fallback - os.freemem() works correctly
  return freemem();
}

// Default provider uses platform-aware available memory
const defaultProvider: MemoryProvider = {
  freemem,
  totalmem,
  availablemem: getAvailableMemoryBytes,
};

// Allow overriding for tests
let memoryProvider: MemoryProvider = defaultProvider;

/**
 * Set a custom memory provider (for testing).
 */
export function setMemoryProvider(provider: MemoryProvider): void {
  memoryProvider = provider;
}

/**
 * Reset to default memory provider (for testing).
 */
export function resetMemoryProvider(): void {
  memoryProvider = defaultProvider;
}

/**
 * Get the percentage of available system memory.
 *
 * Uses platform-aware methods to get true available memory:
 * - macOS: vm_stat (free + inactive + purgeable + speculative)
 * - Linux: /proc/meminfo MemAvailable
 * - Windows/fallback: os.freemem()
 *
 * This is important because os.freemem() only reports truly "free" memory,
 * not memory that's used for caching but immediately reclaimable.
 * On macOS, freemem might report 8% while available is actually 49%.
 *
 * @returns Available memory as a percentage (0-100)
 */
export function getAvailableMemoryPercent(): number {
  const available = memoryProvider.availablemem
    ? memoryProvider.availablemem()
    : memoryProvider.freemem();
  const total = memoryProvider.totalmem();
  return (available / total) * 100;
}

/**
 * Check if system memory is below a given threshold.
 *
 * @param thresholdPercent - Minimum available memory percentage (default: 10)
 * @returns true if memory is LOW (below threshold), false if OK
 */
export function isMemoryLow(thresholdPercent: number = 10): boolean {
  return getAvailableMemoryPercent() < thresholdPercent;
}

/**
 * Get memory info for logging purposes.
 *
 * @returns Object with memory statistics
 */
export function getMemoryInfo(): {
  freeBytes: number;
  availableBytes: number;
  totalBytes: number;
  availablePercent: number;
  freeGB: string;
  availableGB: string;
  totalGB: string;
} {
  const free = memoryProvider.freemem();
  const available = memoryProvider.availablemem
    ? memoryProvider.availablemem()
    : free;
  const total = memoryProvider.totalmem();
  const percent = (available / total) * 100;

  return {
    freeBytes: free,
    availableBytes: available,
    totalBytes: total,
    availablePercent: percent,
    freeGB: (free / (1024 * 1024 * 1024)).toFixed(2),
    availableGB: (available / (1024 * 1024 * 1024)).toFixed(2),
    totalGB: (total / (1024 * 1024 * 1024)).toFixed(2),
  };
}
