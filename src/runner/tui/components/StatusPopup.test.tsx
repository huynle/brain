/**
 * Tests for the StatusPopup component
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { StatusPopup } from './StatusPopup';
import { ENTRY_STATUSES } from '../../../core/types';

describe('StatusPopup', () => {
  describe('rendering', () => {
    it('should render the header with task title', () => {
      const { lastFrame } = render(
        <StatusPopup
          currentStatus="pending"
          selectedStatus="pending"
          taskTitle="My Test Task"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Change Status');
      expect(frame).toContain('My Test Task');
    });

    it('should truncate long task titles', () => {
      const longTitle = 'This is a very long task title that should be truncated for display';
      const { lastFrame } = render(
        <StatusPopup
          currentStatus="pending"
          selectedStatus="pending"
          taskTitle={longTitle}
        />
      );

      const frame = lastFrame();
      // Title should be truncated with ellipsis
      expect(frame).toContain('...');
      expect(frame).not.toContain(longTitle);
    });

    it('should render all status options', () => {
      const { lastFrame } = render(
        <StatusPopup
          currentStatus="pending"
          selectedStatus="pending"
          taskTitle="Test Task"
        />
      );

      const frame = lastFrame();
      
      // Check that all statuses are rendered
      for (const status of ENTRY_STATUSES) {
        // Status labels have underscores replaced with spaces
        const label = status.replace('_', ' ');
        expect(frame).toContain(label);
      }
    });

    it('should show keyboard shortcuts in footer', () => {
      const { lastFrame } = render(
        <StatusPopup
          currentStatus="pending"
          selectedStatus="pending"
          taskTitle="Test Task"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('j/k');
      expect(frame).toContain('Navigate');
      expect(frame).toContain('Enter');
      expect(frame).toContain('Select');
      expect(frame).toContain('Esc');
      expect(frame).toContain('Cancel');
    });
  });

  describe('current status indication', () => {
    it('should mark the current status with filled circle', () => {
      const { lastFrame } = render(
        <StatusPopup
          currentStatus="completed"
          selectedStatus="pending"
          taskTitle="Test Task"
        />
      );

      const frame = lastFrame();
      // The current status should have (current) indicator
      expect(frame).toContain('(current)');
    });

    it('should show (current) marker for current status', () => {
      const { lastFrame } = render(
        <StatusPopup
          currentStatus="in_progress"
          selectedStatus="pending"
          taskTitle="Test Task"
        />
      );

      const frame = lastFrame();
      // in_progress should be marked as current
      expect(frame).toMatch(/in progress.*\(current\)/);
    });
  });

  describe('selection indication', () => {
    it('should show arrow indicator for selected status', () => {
      const { lastFrame } = render(
        <StatusPopup
          currentStatus="pending"
          selectedStatus="completed"
          taskTitle="Test Task"
        />
      );

      const frame = lastFrame();
      // The selected status should have an arrow
      expect(frame).toContain('→');
    });

    it('should highlight selected status differently from current', () => {
      const { lastFrame } = render(
        <StatusPopup
          currentStatus="pending"
          selectedStatus="blocked"
          taskTitle="Test Task"
        />
      );

      const frame = lastFrame();
      // Both should be visible, with different indicators
      expect(frame).toContain('(current)');  // pending marked as current
      expect(frame).toContain('→');           // blocked has selection arrow
    });
  });

  describe('status labels', () => {
    it('should display in_progress as "in progress" (with space)', () => {
      const { lastFrame } = render(
        <StatusPopup
          currentStatus="in_progress"
          selectedStatus="in_progress"
          taskTitle="Test Task"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('in progress');
    });
  });
});
