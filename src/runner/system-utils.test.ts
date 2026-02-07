/**
 * Tests for system-utils module
 *
 * Tests the platform-aware memory detection that uses:
 * - macOS: vm_stat (free + inactive + purgeable + speculative)
 * - Linux: /proc/meminfo MemAvailable
 * - Windows/fallback: os.freemem()
 */

import { describe, it, expect, beforeEach, afterEach } from "bun:test";
import {
  getAvailableMemoryPercent,
  isMemoryLow,
  getMemoryInfo,
  getProcessResourceUsage,
  setMemoryProvider,
  resetMemoryProvider,
  type MemoryProvider,
} from "./system-utils";

describe("system-utils", () => {
  afterEach(() => {
    resetMemoryProvider();
  });

  /**
   * Create a mock provider with optional availablemem.
   * When availablemem is provided, it simulates macOS/Linux where available >> free.
   */
  const createMockProvider = (
    freeBytes: number,
    totalBytes: number,
    availableBytes?: number
  ): MemoryProvider => ({
    freemem: () => freeBytes,
    totalmem: () => totalBytes,
    ...(availableBytes !== undefined && { availablemem: () => availableBytes }),
  });

  describe("getAvailableMemoryPercent", () => {
    it("should return correct percentage when memory is at 50%", () => {
      setMemoryProvider(
        createMockProvider(
          8 * 1024 * 1024 * 1024, // 8GB free
          16 * 1024 * 1024 * 1024, // 16GB total
          8 * 1024 * 1024 * 1024 // 8GB available (same as free for this test)
        )
      );

      const result = getAvailableMemoryPercent();
      expect(result).toBe(50);
    });

    it("should return correct percentage when memory is at 10%", () => {
      setMemoryProvider(
        createMockProvider(
          1.6 * 1024 * 1024 * 1024, // 1.6GB free
          16 * 1024 * 1024 * 1024, // 16GB total
          1.6 * 1024 * 1024 * 1024 // 1.6GB available
        )
      );

      const result = getAvailableMemoryPercent();
      expect(result).toBe(10);
    });

    it("should return correct percentage when memory is at 5%", () => {
      setMemoryProvider(
        createMockProvider(
          0.8 * 1024 * 1024 * 1024, // 0.8GB free
          16 * 1024 * 1024 * 1024, // 16GB total
          0.8 * 1024 * 1024 * 1024 // 0.8GB available
        )
      );

      const result = getAvailableMemoryPercent();
      expect(result).toBe(5);
    });

    it("should handle very low memory", () => {
      setMemoryProvider(
        createMockProvider(
          100 * 1024 * 1024, // 100MB free
          16 * 1024 * 1024 * 1024, // 16GB total
          100 * 1024 * 1024 // 100MB available
        )
      );

      const result = getAvailableMemoryPercent();
      expect(result).toBeCloseTo(0.61, 1); // ~0.61%
    });

    it("should handle 100% free memory", () => {
      setMemoryProvider(
        createMockProvider(
          16 * 1024 * 1024 * 1024,
          16 * 1024 * 1024 * 1024,
          16 * 1024 * 1024 * 1024
        )
      );

      const result = getAvailableMemoryPercent();
      expect(result).toBe(100);
    });

    it("should use availablemem when provided (macOS/Linux scenario)", () => {
      // Simulate macOS where free is low but available is high
      setMemoryProvider(
        createMockProvider(
          3 * 1024 * 1024 * 1024, // 3GB free (what os.freemem would report)
          36 * 1024 * 1024 * 1024, // 36GB total
          18 * 1024 * 1024 * 1024 // 18GB available (includes inactive + purgeable)
        )
      );

      const result = getAvailableMemoryPercent();
      expect(result).toBe(50); // 18/36 = 50%, not 3/36 = 8.3%
    });

    it("should fall back to freemem when availablemem not provided", () => {
      // Simulate Windows where availablemem is not set
      setMemoryProvider(
        createMockProvider(
          8 * 1024 * 1024 * 1024, // 8GB free
          16 * 1024 * 1024 * 1024 // 16GB total
          // no availablemem - should fall back to freemem
        )
      );

      const result = getAvailableMemoryPercent();
      expect(result).toBe(50);
    });
  });

  describe("isMemoryLow", () => {
    it("should return true when memory is below threshold", () => {
      setMemoryProvider(
        createMockProvider(
          0.5 * 1024 * 1024 * 1024, // 0.5GB (3.125%)
          16 * 1024 * 1024 * 1024, // 16GB
          0.5 * 1024 * 1024 * 1024
        )
      );

      expect(isMemoryLow(10)).toBe(true);
    });

    it("should return false when memory is above threshold", () => {
      setMemoryProvider(
        createMockProvider(
          4 * 1024 * 1024 * 1024, // 4GB (25%)
          16 * 1024 * 1024 * 1024, // 16GB
          4 * 1024 * 1024 * 1024
        )
      );

      expect(isMemoryLow(10)).toBe(false);
    });

    it("should return false when memory is exactly at threshold", () => {
      setMemoryProvider(
        createMockProvider(
          1.6 * 1024 * 1024 * 1024, // 1.6GB (10%)
          16 * 1024 * 1024 * 1024, // 16GB
          1.6 * 1024 * 1024 * 1024
        )
      );

      // Exactly at threshold means NOT below, so not low
      expect(isMemoryLow(10)).toBe(false);
    });

    it("should use default threshold of 10 when not specified", () => {
      setMemoryProvider(
        createMockProvider(
          0.8 * 1024 * 1024 * 1024, // 0.8GB (5%)
          16 * 1024 * 1024 * 1024, // 16GB
          0.8 * 1024 * 1024 * 1024
        )
      );

      expect(isMemoryLow()).toBe(true);
    });

    it("should work with custom threshold", () => {
      setMemoryProvider(
        createMockProvider(
          4 * 1024 * 1024 * 1024, // 4GB (25%)
          16 * 1024 * 1024 * 1024, // 16GB
          4 * 1024 * 1024 * 1024
        )
      );

      expect(isMemoryLow(30)).toBe(true); // 25% < 30%
      expect(isMemoryLow(20)).toBe(false); // 25% > 20%
    });

    it("should use available memory to determine low status (macOS scenario)", () => {
      // Simulate macOS: free is low (8%) but available is high (50%)
      setMemoryProvider(
        createMockProvider(
          3 * 1024 * 1024 * 1024, // 3GB free (would trigger false positive)
          36 * 1024 * 1024 * 1024, // 36GB total
          18 * 1024 * 1024 * 1024 // 18GB available (50%)
        )
      );

      // With old implementation: 3/36 = 8.3% < 10% => would incorrectly say LOW
      // With new implementation: 18/36 = 50% > 10% => correctly says NOT low
      expect(isMemoryLow(10)).toBe(false);
    });
  });

  describe("getMemoryInfo", () => {
    it("should return detailed memory info", () => {
      const free = 4 * 1024 * 1024 * 1024; // 4GB
      const available = 8 * 1024 * 1024 * 1024; // 8GB available (includes cache)
      const total = 16 * 1024 * 1024 * 1024; // 16GB

      setMemoryProvider(createMockProvider(free, total, available));

      const info = getMemoryInfo();

      expect(info.freeBytes).toBe(free);
      expect(info.availableBytes).toBe(available);
      expect(info.totalBytes).toBe(total);
      expect(info.availablePercent).toBe(50); // 8/16 = 50%
      expect(info.freeGB).toBe("4.00");
      expect(info.availableGB).toBe("8.00");
      expect(info.totalGB).toBe("16.00");
    });

    it("should format GB with 2 decimal places", () => {
      const free = 5.5 * 1024 * 1024 * 1024; // 5.5GB
      const available = 12.25 * 1024 * 1024 * 1024; // 12.25GB
      const total = 32.75 * 1024 * 1024 * 1024; // 32.75GB

      setMemoryProvider(createMockProvider(free, total, available));

      const info = getMemoryInfo();

      expect(info.freeGB).toBe("5.50");
      expect(info.availableGB).toBe("12.25");
      expect(info.totalGB).toBe("32.75");
    });

    it("should show difference between free and available memory", () => {
      // Simulate macOS: free is low due to cache, but available is high
      const free = 3 * 1024 * 1024 * 1024; // 3GB free (os.freemem)
      const available = 18 * 1024 * 1024 * 1024; // 18GB available (vm_stat)
      const total = 36 * 1024 * 1024 * 1024; // 36GB total

      setMemoryProvider(createMockProvider(free, total, available));

      const info = getMemoryInfo();

      // Shows both metrics - useful for logging/debugging
      expect(info.freeBytes).toBe(free);
      expect(info.availableBytes).toBe(available);
      expect(info.freeGB).toBe("3.00");
      expect(info.availableGB).toBe("18.00");
      // availablePercent uses available, not free
      expect(info.availablePercent).toBe(50);
    });

    it("should use freemem as available when availablemem not provided", () => {
      const free = 4 * 1024 * 1024 * 1024; // 4GB
      const total = 16 * 1024 * 1024 * 1024; // 16GB

      // No availablemem - simulates Windows fallback
      setMemoryProvider(createMockProvider(free, total));

      const info = getMemoryInfo();

      expect(info.freeBytes).toBe(free);
      expect(info.availableBytes).toBe(free); // Falls back to free
      expect(info.availablePercent).toBe(25);
    });
  });

  describe("memory provider", () => {
    it("should reset to default provider correctly", () => {
      // Get a reading from default provider (real system memory)
      const realReading = getAvailableMemoryPercent();
      expect(realReading).toBeGreaterThan(0);
      expect(realReading).toBeLessThanOrEqual(100);

      // Override with mock
      setMemoryProvider(createMockProvider(
        8 * 1024 * 1024 * 1024,
        16 * 1024 * 1024 * 1024
      ));
      expect(getAvailableMemoryPercent()).toBe(50);

      // Reset should restore real readings
      resetMemoryProvider();
      const afterReset = getAvailableMemoryPercent();
      expect(afterReset).toBeGreaterThan(0);
      expect(afterReset).toBeLessThanOrEqual(100);
    });
  });

  describe("getProcessResourceUsage", () => {
    it("should return zeros for empty PID array", () => {
      const result = getProcessResourceUsage([]);
      expect(result.cpuPercent).toBe(0);
      expect(result.memoryBytes).toBe(0);
      expect(result.memoryMB).toBe("0");
      expect(result.processCount).toBe(0);
    });

    it("should return zeros for invalid PIDs (0, negative)", () => {
      const result = getProcessResourceUsage([0, -1, -100]);
      expect(result.cpuPercent).toBe(0);
      expect(result.memoryBytes).toBe(0);
      expect(result.memoryMB).toBe("0");
      expect(result.processCount).toBe(0);
    });

    it("should return zeros for non-existent PIDs", () => {
      // Use very high PIDs that are unlikely to exist
      const result = getProcessResourceUsage([999999999, 999999998]);
      expect(result.cpuPercent).toBe(0);
      expect(result.memoryBytes).toBe(0);
      expect(result.memoryMB).toBe("0");
      expect(result.processCount).toBe(0);
    });

    it("should measure current process", () => {
      // process.pid is always valid
      const result = getProcessResourceUsage([process.pid]);
      
      // Current process should have some CPU and memory
      expect(result.processCount).toBe(1);
      expect(result.cpuPercent).toBeGreaterThanOrEqual(0);
      expect(result.memoryBytes).toBeGreaterThan(0);
      expect(parseFloat(result.memoryMB)).toBeGreaterThan(0);
    });

    it("should aggregate multiple processes", () => {
      // Measure current process and parent process (if available)
      const pids = [process.pid];
      if (process.ppid > 0) {
        pids.push(process.ppid);
      }

      const result = getProcessResourceUsage(pids);
      
      // Should measure at least one process
      expect(result.processCount).toBeGreaterThanOrEqual(1);
      expect(result.memoryBytes).toBeGreaterThan(0);
    });

    it("should filter out zero and negative PIDs before measuring", () => {
      // Mix of valid and invalid PIDs (only 0 and negative are filtered)
      // Note: Very large PIDs may cause ps to fail on some systems
      const result = getProcessResourceUsage([0, process.pid, -1]);
      
      // Should only measure the valid PID
      expect(result.processCount).toBe(1);
      expect(result.memoryBytes).toBeGreaterThan(0);
    });

    it("should format memoryMB with 1 decimal place", () => {
      const result = getProcessResourceUsage([process.pid]);
      
      // Check format is X.X (one decimal place)
      const parts = result.memoryMB.split(".");
      expect(parts.length).toBe(2);
      expect(parts[1].length).toBe(1);
    });
  });
});
