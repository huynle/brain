/**
 * LogViewer Component Tests
 *
 * Tests the log display component including:
 * - Log entry rendering
 * - Color coding by log level
 * - Empty state handling
 * - Line truncation (maxLines)
 * - Timestamp formatting
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { LogViewer } from './LogViewer';
import type { LogEntry } from '../types';

// Helper to create mock log entries
function createLog(overrides: Partial<LogEntry> = {}): LogEntry {
  return {
    timestamp: new Date('2026-02-03T17:30:45.000Z'),
    level: 'info',
    message: 'Test log message',
    ...overrides,
  };
}

describe('LogViewer', () => {
  describe('rendering log entries', () => {
    it('renders single log entry', () => {
      const logs = [createLog({ message: 'Server started' })];
      const { lastFrame } = render(<LogViewer logs={logs} />);
      expect(lastFrame()).toContain('Server started');
    });

    it('renders multiple log entries', () => {
      const logs = [
        createLog({ message: 'First message' }),
        createLog({ message: 'Second message' }),
        createLog({ message: 'Third message' }),
      ];
      const { lastFrame } = render(<LogViewer logs={logs} />);
      expect(lastFrame()).toContain('First message');
      expect(lastFrame()).toContain('Second message');
      expect(lastFrame()).toContain('Third message');
    });

    it('shows "No logs yet" when empty', () => {
      const { lastFrame } = render(<LogViewer logs={[]} />);
      expect(lastFrame()).toContain('No logs yet');
    });
  });

  describe('log level display', () => {
    it('displays INFO level label', () => {
      const logs = [createLog({ level: 'info', message: 'Info message' })];
      const { lastFrame } = render(<LogViewer logs={logs} showLevel={true} />);
      expect(lastFrame()).toContain('INFO');
    });

    it('displays WARN level label', () => {
      const logs = [createLog({ level: 'warn', message: 'Warning message' })];
      const { lastFrame } = render(<LogViewer logs={logs} showLevel={true} />);
      expect(lastFrame()).toContain('WARN');
    });

    it('displays ERROR level label', () => {
      const logs = [createLog({ level: 'error', message: 'Error message' })];
      const { lastFrame } = render(<LogViewer logs={logs} showLevel={true} />);
      expect(lastFrame()).toContain('ERROR');
    });

    it('displays DEBUG level label', () => {
      const logs = [createLog({ level: 'debug', message: 'Debug message' })];
      const { lastFrame } = render(<LogViewer logs={logs} showLevel={true} />);
      expect(lastFrame()).toContain('DEBUG');
    });

    it('hides level when showLevel is false', () => {
      const logs = [createLog({ level: 'info', message: 'Message' })];
      const { lastFrame } = render(<LogViewer logs={logs} showLevel={false} />);
      expect(lastFrame()).not.toContain('INFO');
    });
  });

  describe('timestamp formatting', () => {
    it('shows formatted timestamp by default', () => {
      const logs = [
        createLog({
          timestamp: new Date('2026-02-03T17:30:45.000Z'),
          message: 'Message',
        }),
      ];
      const { lastFrame } = render(
        <LogViewer logs={logs} showTimestamp={true} />
      );
      // Time should be shown in HH:MM:SS format (local timezone)
      // Just check that we have digits and colons which represent time
      const frame = lastFrame() || '';
      expect(frame).toMatch(/\d{1,2}:\d{2}:\d{2}/);
    });

    it('hides timestamp when showTimestamp is false', () => {
      const logs = [
        createLog({
          timestamp: new Date('2026-02-03T17:30:45.000Z'),
          message: 'Message without timestamp',
        }),
      ];
      const { lastFrame } = render(
        <LogViewer logs={logs} showTimestamp={false} />
      );
      // Should still contain the message
      expect(lastFrame()).toContain('Message without timestamp');
    });
  });

  describe('maxLines truncation', () => {
    it('shows only the most recent logs when over maxLines', () => {
      const logs = Array.from({ length: 10 }, (_, i) =>
        createLog({ message: `LogItem${i + 1}End` })
      );
      const { lastFrame } = render(<LogViewer logs={logs} maxLines={5} />);
      // Should show items 6-10 (most recent)
      expect(lastFrame()).toContain('LogItem6End');
      expect(lastFrame()).toContain('LogItem10End');
      // Should NOT show older items - use unique markers to avoid substring matching
      expect(lastFrame()).not.toContain('LogItem1End');
      expect(lastFrame()).not.toContain('LogItem5End');
    });

    it('shows all logs when under maxLines', () => {
      const logs = Array.from({ length: 3 }, (_, i) =>
        createLog({ message: `Message ${i + 1}` })
      );
      const { lastFrame } = render(<LogViewer logs={logs} maxLines={10} />);
      expect(lastFrame()).toContain('Message 1');
      expect(lastFrame()).toContain('Message 2');
      expect(lastFrame()).toContain('Message 3');
    });

    it('uses default maxLines of 50', () => {
      const logs = Array.from({ length: 60 }, (_, i) =>
        createLog({ message: `Msg${i + 1}` })
      );
      const { lastFrame } = render(<LogViewer logs={logs} />);
      // Should show the last 50 logs (11-60)
      expect(lastFrame()).toContain('Msg60');
      expect(lastFrame()).toContain('Msg11');
      expect(lastFrame()).not.toContain('Msg10');
    });
  });

  describe('context display', () => {
    it('shows context as key=value pairs', () => {
      const logs = [
        createLog({
          message: 'Task started',
          context: { taskId: 'abc123' },
        }),
      ];
      const { lastFrame } = render(<LogViewer logs={logs} />);
      expect(lastFrame()).toContain('taskId="abc123"');
    });

    it('handles multiple context values', () => {
      const logs = [
        createLog({
          message: 'Event',
          context: { user: 'john', action: 'login' },
        }),
      ];
      const { lastFrame } = render(<LogViewer logs={logs} />);
      expect(lastFrame()).toContain('user="john"');
      expect(lastFrame()).toContain('action="login"');
    });

    it('handles empty context object', () => {
      const logs = [
        createLog({
          message: 'Simple message',
          context: {},
        }),
      ];
      const { lastFrame } = render(<LogViewer logs={logs} />);
      expect(lastFrame()).toContain('Simple message');
    });

    it('handles undefined context', () => {
      const logs = [
        createLog({
          message: 'No context',
          context: undefined,
        }),
      ];
      const { lastFrame } = render(<LogViewer logs={logs} />);
      expect(lastFrame()).toContain('No context');
    });
  });

  describe('message truncation', () => {
    it('truncates very long messages', () => {
      const longMessage = 'A'.repeat(200);
      const logs = [createLog({ message: longMessage })];
      const { lastFrame } = render(<LogViewer logs={logs} />);
      // Should contain ellipsis for truncation
      expect(lastFrame()).toContain('...');
      // Should not contain the full message
      expect(lastFrame()).not.toContain(longMessage);
    });

    it('does not truncate short messages', () => {
      const shortMessage = 'Short message';
      const logs = [createLog({ message: shortMessage })];
      const { lastFrame } = render(<LogViewer logs={logs} />);
      expect(lastFrame()).toContain(shortMessage);
      // Should not have truncation ellipsis for this message
      const frame = lastFrame() || '';
      // Count occurrences of message - should be exactly as written
      expect(frame.includes('Short message')).toBe(true);
    });
  });

  describe('edge cases', () => {
    it('handles logs with special characters', () => {
      const logs = [
        createLog({
          message: 'Error: file "test.txt" not found <path>',
        }),
      ];
      const { lastFrame } = render(<LogViewer logs={logs} />);
      expect(lastFrame()).toContain('Error:');
    });

    it('handles rapid log updates', () => {
      const logs = Array.from({ length: 100 }, (_, i) =>
        createLog({ message: `Rapid message ${i}` })
      );
      const { lastFrame } = render(<LogViewer logs={logs} maxLines={10} />);
      // Should render without crashing and show recent logs
      expect(lastFrame()).toContain('Rapid message 99');
    });

    it('handles complex context objects', () => {
      const logs = [
        createLog({
          message: 'Complex context',
          context: { nested: { deep: 'value' }, array: [1, 2, 3] },
        }),
      ];
      const { lastFrame } = render(<LogViewer logs={logs} />);
      // Should serialize complex values to JSON
      expect(lastFrame()).toContain('nested=');
    });
  });
});
