/**
 * System Utilities
 *
 * Utility functions for system resource monitoring.
 */

import { freemem, totalmem } from "os";

/**
 * Memory provider interface for dependency injection in tests.
 */
export interface MemoryProvider {
  freemem: () => number;
  totalmem: () => number;
}

// Default provider uses Node.js os module
const defaultProvider: MemoryProvider = {
  freemem,
  totalmem,
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
 * Get the percentage of available (free) system memory.
 *
 * Uses Node.js os module to query system memory:
 * - freemem(): Available memory in bytes
 * - totalmem(): Total system memory in bytes
 *
 * @returns Available memory as a percentage (0-100)
 */
export function getAvailableMemoryPercent(): number {
  const free = memoryProvider.freemem();
  const total = memoryProvider.totalmem();
  return (free / total) * 100;
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
  totalBytes: number;
  availablePercent: number;
  freeGB: string;
  totalGB: string;
} {
  const free = memoryProvider.freemem();
  const total = memoryProvider.totalmem();
  const percent = (free / total) * 100;

  return {
    freeBytes: free,
    totalBytes: total,
    availablePercent: percent,
    freeGB: (free / (1024 * 1024 * 1024)).toFixed(2),
    totalGB: (total / (1024 * 1024 * 1024)).toFixed(2),
  };
}
