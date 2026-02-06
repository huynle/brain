/**
 * Tests for system-utils module
 */

import { describe, it, expect, beforeEach, afterEach } from "bun:test";
import {
  getAvailableMemoryPercent,
  isMemoryLow,
  getMemoryInfo,
  setMemoryProvider,
  resetMemoryProvider,
  type MemoryProvider,
} from "./system-utils";

describe("system-utils", () => {
  afterEach(() => {
    resetMemoryProvider();
  });

  const createMockProvider = (freeBytes: number, totalBytes: number): MemoryProvider => ({
    freemem: () => freeBytes,
    totalmem: () => totalBytes,
  });

  describe("getAvailableMemoryPercent", () => {
    it("should return correct percentage when memory is at 50%", () => {
      setMemoryProvider(createMockProvider(
        8 * 1024 * 1024 * 1024, // 8GB free
        16 * 1024 * 1024 * 1024 // 16GB total
      ));

      const result = getAvailableMemoryPercent();
      expect(result).toBe(50);
    });

    it("should return correct percentage when memory is at 10%", () => {
      setMemoryProvider(createMockProvider(
        1.6 * 1024 * 1024 * 1024, // 1.6GB free
        16 * 1024 * 1024 * 1024 // 16GB total
      ));

      const result = getAvailableMemoryPercent();
      expect(result).toBe(10);
    });

    it("should return correct percentage when memory is at 5%", () => {
      setMemoryProvider(createMockProvider(
        0.8 * 1024 * 1024 * 1024, // 0.8GB free
        16 * 1024 * 1024 * 1024 // 16GB total
      ));

      const result = getAvailableMemoryPercent();
      expect(result).toBe(5);
    });

    it("should handle very low memory", () => {
      setMemoryProvider(createMockProvider(
        100 * 1024 * 1024, // 100MB free
        16 * 1024 * 1024 * 1024 // 16GB total
      ));

      const result = getAvailableMemoryPercent();
      expect(result).toBeCloseTo(0.61, 1); // ~0.61%
    });

    it("should handle 100% free memory", () => {
      setMemoryProvider(createMockProvider(
        16 * 1024 * 1024 * 1024,
        16 * 1024 * 1024 * 1024
      ));

      const result = getAvailableMemoryPercent();
      expect(result).toBe(100);
    });
  });

  describe("isMemoryLow", () => {
    it("should return true when memory is below threshold", () => {
      setMemoryProvider(createMockProvider(
        0.5 * 1024 * 1024 * 1024, // 0.5GB (3.125%)
        16 * 1024 * 1024 * 1024 // 16GB
      ));

      expect(isMemoryLow(10)).toBe(true);
    });

    it("should return false when memory is above threshold", () => {
      setMemoryProvider(createMockProvider(
        4 * 1024 * 1024 * 1024, // 4GB (25%)
        16 * 1024 * 1024 * 1024 // 16GB
      ));

      expect(isMemoryLow(10)).toBe(false);
    });

    it("should return false when memory is exactly at threshold", () => {
      setMemoryProvider(createMockProvider(
        1.6 * 1024 * 1024 * 1024, // 1.6GB (10%)
        16 * 1024 * 1024 * 1024 // 16GB
      ));

      // Exactly at threshold means NOT below, so not low
      expect(isMemoryLow(10)).toBe(false);
    });

    it("should use default threshold of 10 when not specified", () => {
      setMemoryProvider(createMockProvider(
        0.8 * 1024 * 1024 * 1024, // 0.8GB (5%)
        16 * 1024 * 1024 * 1024 // 16GB
      ));

      expect(isMemoryLow()).toBe(true);
    });

    it("should work with custom threshold", () => {
      setMemoryProvider(createMockProvider(
        4 * 1024 * 1024 * 1024, // 4GB (25%)
        16 * 1024 * 1024 * 1024 // 16GB
      ));

      expect(isMemoryLow(30)).toBe(true); // 25% < 30%
      expect(isMemoryLow(20)).toBe(false); // 25% > 20%
    });
  });

  describe("getMemoryInfo", () => {
    it("should return detailed memory info", () => {
      const free = 4 * 1024 * 1024 * 1024; // 4GB
      const total = 16 * 1024 * 1024 * 1024; // 16GB

      setMemoryProvider(createMockProvider(free, total));

      const info = getMemoryInfo();

      expect(info.freeBytes).toBe(free);
      expect(info.totalBytes).toBe(total);
      expect(info.availablePercent).toBe(25);
      expect(info.freeGB).toBe("4.00");
      expect(info.totalGB).toBe("16.00");
    });

    it("should format GB with 2 decimal places", () => {
      const free = 5.5 * 1024 * 1024 * 1024; // 5.5GB
      const total = 32.75 * 1024 * 1024 * 1024; // 32.75GB

      setMemoryProvider(createMockProvider(free, total));

      const info = getMemoryInfo();

      expect(info.freeGB).toBe("5.50");
      expect(info.totalGB).toBe("32.75");
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
});
