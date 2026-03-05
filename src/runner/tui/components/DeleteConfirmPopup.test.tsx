/**
 * Tests for the DeleteConfirmPopup component
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { DeleteConfirmPopup } from './DeleteConfirmPopup';

describe('DeleteConfirmPopup', () => {
  describe('rendering', () => {
    it('should render the header', () => {
      const { lastFrame } = render(
        <DeleteConfirmPopup taskTitles={['Test Task']} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Delete Tasks');
    });

    it('should show singular task count', () => {
      const { lastFrame } = render(
        <DeleteConfirmPopup taskTitles={['Single Task']} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Delete');
      expect(frame).toContain('1');
      expect(frame).toContain('task?');
      // Should not have 's' for plural
      expect(frame).not.toContain('tasks?');
    });

    it('should show plural task count', () => {
      const { lastFrame } = render(
        <DeleteConfirmPopup taskTitles={['Task 1', 'Task 2', 'Task 3']} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Delete');
      expect(frame).toContain('3');
      expect(frame).toContain('tasks?');
    });

    it('should display task titles', () => {
      const titles = ['Fix login bug', 'Add dark mode', 'Update documentation'];
      const { lastFrame } = render(
        <DeleteConfirmPopup taskTitles={titles} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Fix login bug');
      expect(frame).toContain('Add dark mode');
      expect(frame).toContain('Update documentation');
    });

    it('should truncate when more than 5 tasks', () => {
      const titles = [
        'Task 1', 'Task 2', 'Task 3', 'Task 4', 'Task 5',
        'Task 6', 'Task 7'
      ];
      const { lastFrame } = render(
        <DeleteConfirmPopup taskTitles={titles} />
      );

      const frame = lastFrame();
      // First 5 should be visible
      expect(frame).toContain('Task 1');
      expect(frame).toContain('Task 5');
      // Last 2 should be hidden
      expect(frame).not.toContain('Task 6');
      expect(frame).not.toContain('Task 7');
      // Should show "and 2 more"
      expect(frame).toContain('and 2 more');
    });

    it('should truncate long task titles', () => {
      const longTitle = 'This is a very long task title that exceeds the maximum allowed length for display';
      const { lastFrame } = render(
        <DeleteConfirmPopup taskTitles={[longTitle]} />
      );

      const frame = lastFrame();
      expect(frame).toContain('...');
      expect(frame).not.toContain(longTitle);
    });

    it('should show warning message', () => {
      const { lastFrame } = render(
        <DeleteConfirmPopup taskTitles={['Test Task']} />
      );

      const frame = lastFrame();
      expect(frame).toContain('This action cannot be undone');
    });

    it('should show keyboard shortcuts', () => {
      const { lastFrame } = render(
        <DeleteConfirmPopup taskTitles={['Test Task']} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Enter: Confirm');
      expect(frame).toContain('Esc: Cancel');
    });
  });

  describe('feature mode', () => {
    it('should show feature ID when provided', () => {
      const { lastFrame } = render(
        <DeleteConfirmPopup
          taskTitles={['Task 1', 'Task 2']}
          featureId="dark-mode"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('feature: dark-mode');
    });

    it('should not show feature label when not provided', () => {
      const { lastFrame } = render(
        <DeleteConfirmPopup taskTitles={['Task 1']} />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('feature:');
    });
  });

  describe('edge cases', () => {
    it('should handle empty task list', () => {
      const { lastFrame } = render(
        <DeleteConfirmPopup taskTitles={[]} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Delete');
      expect(frame).toContain('0');
      expect(frame).toContain('tasks?');
    });

    it('should handle exactly 5 tasks without truncation message', () => {
      const titles = ['Task 1', 'Task 2', 'Task 3', 'Task 4', 'Task 5'];
      const { lastFrame } = render(
        <DeleteConfirmPopup taskTitles={titles} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Task 1');
      expect(frame).toContain('Task 5');
      expect(frame).not.toContain('more');
    });
  });
});
