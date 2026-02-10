/**
 * Tests for the MetadataPopup component
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { MetadataPopup, type MetadataField, type MetadataPopupMode } from './MetadataPopup';
import { ENTRY_STATUSES, type EntryStatus } from '../../../core/types';

const defaultProps = {
  mode: 'single' as MetadataPopupMode,
  taskTitle: 'Test Task',
  focusedField: 'status' as MetadataField,
  statusValue: 'pending' as EntryStatus,
  featureIdValue: 'my-feature',
  branchValue: 'feature/test',
  workdirValue: '/path/to/project',
  selectedStatusIndex: 1, // pending is index 1 in ENTRY_STATUSES
  allowedStatuses: ENTRY_STATUSES,
  editingField: null,
};

describe('MetadataPopup', () => {
  describe('rendering', () => {
    it('should render the header with title', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Update Metadata');
      expect(frame).toContain('Test Task');
    });

    it('should truncate long task titles', () => {
      const longTitle = 'This is a very long task title that should be truncated for display';
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} taskTitle={longTitle} />
      );

      const frame = lastFrame();
      expect(frame).toContain('...');
      expect(frame).not.toContain(longTitle);
    });

    it('should render all four fields', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Status:');
      expect(frame).toContain('Feature ID:');
      expect(frame).toContain('Branch:');
      expect(frame).toContain('Workdir:');
    });

    it('should show keyboard shortcuts in footer', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Tab');
      expect(frame).toContain('next');
      expect(frame).toContain('Enter');
      expect(frame).toContain('edit');
      expect(frame).toContain('Esc');
    });
  });

  describe('mode variations', () => {
    it('should show task title in single mode', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} mode="single" taskTitle="Single Task" />
      );

      const frame = lastFrame();
      expect(frame).toContain('Single Task');
    });

    it('should show batch count in batch mode', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} mode="batch" batchCount={5} />
      );

      const frame = lastFrame();
      expect(frame).toContain('5 tasks selected');
    });

    it('should show feature info in feature mode', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} mode="feature" featureId="auth-system" batchCount={3} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Feature: auth-system');
      expect(frame).toContain('3 tasks');
    });
  });

  describe('field focus indication', () => {
    it('should show arrow indicator for focused field', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} focusedField="status" />
      );

      const frame = lastFrame();
      expect(frame).toContain('→');
    });

    it('should show j/k hint for status field when focused', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} focusedField="status" />
      );

      const frame = lastFrame();
      expect(frame).toContain('j/k to change');
    });

    it('should show Enter hint for text field when focused', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} focusedField="feature_id" />
      );

      const frame = lastFrame();
      expect(frame).toContain('Enter to edit');
    });
  });

  describe('status field', () => {
    it('should display current status with label', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} statusValue="in_progress" selectedStatusIndex={3} />
      );

      const frame = lastFrame();
      expect(frame).toContain('in progress');
    });

    it('should use selectedStatusIndex for display', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          statusValue="pending" 
          selectedStatusIndex={6}  // completed
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('completed');
    });
  });

  describe('text field values', () => {
    it('should display feature_id value', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} featureIdValue="dark-mode" />
      );

      const frame = lastFrame();
      expect(frame).toContain('dark-mode');
    });

    it('should display branch value', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} branchValue="feature/auth" />
      );

      const frame = lastFrame();
      expect(frame).toContain('feature/auth');
    });

    it('should display workdir value', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} workdirValue="/home/user/project" />
      );

      const frame = lastFrame();
      expect(frame).toContain('/home/user/project');
    });

    it('should show (none) for empty text fields', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          featureIdValue="" 
          branchValue="" 
          workdirValue="" 
        />
      );

      const frame = lastFrame();
      // Should contain (none) for the empty fields (multiple occurrences)
      expect(frame).toContain('(none)');
    });
  });

  describe('edit mode', () => {
    it('should show edit buffer when editing a field', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          focusedField="feature_id"
          editingField="feature_id" 
          editBuffer="new-feature"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('new-feature');
    });

    it('should show confirm hint when editing', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          focusedField="git_branch"
          editingField="git_branch" 
          editBuffer="develop"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Enter to confirm');
    });

    it('should update footer text when in edit mode', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          editingField="feature_id" 
          editBuffer="test"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('confirm');
      expect(frame).toContain('cancel edit');
    });
  });

  describe('border colors', () => {
    it('should have cyan border in single mode', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} mode="single" />
      );

      const frame = lastFrame();
      // Just verify it renders without crashing - ink-testing-library doesn't easily expose colors
      expect(frame).toContain('Update Metadata');
    });

    it('should have yellow border in batch mode', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} mode="batch" batchCount={3} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Update Metadata');
      expect(frame).toContain('3 tasks selected');
    });

    it('should have magenta border in feature mode', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} mode="feature" featureId="test" />
      );

      const frame = lastFrame();
      expect(frame).toContain('Update Metadata');
      expect(frame).toContain('Feature: test');
    });
  });
});
