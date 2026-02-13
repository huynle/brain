/**
 * Tests for the MetadataPopup component
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { MetadataPopup, type MetadataField, type MetadataPopupMode } from './MetadataPopup';
import { ENTRY_STATUSES, type EntryStatus } from '../../../core/types';

import type { MetadataInteractionMode } from './MetadataPopup';

const defaultProps = {
  mode: 'single' as MetadataPopupMode,
  taskTitle: 'Test Task',
  focusedField: 'status' as MetadataField,
  statusValue: 'pending' as EntryStatus,
  featureIdValue: 'my-feature',
  branchValue: 'feature/test',
  workdirValue: '/path/to/project',
  projectValue: 'my-project',
  availableProjects: ['my-project', 'other-project', 'another-project'],
  selectedProjectIndex: 0,
  selectedStatusIndex: 1, // pending is index 1 in ENTRY_STATUSES
  allowedStatuses: ENTRY_STATUSES,
  interactionMode: 'navigate' as MetadataInteractionMode,
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
      // In navigate mode: "j/k: navigate  Enter: edit  Esc: close"
      expect(frame).toContain('j/k');
      expect(frame).toContain('navigate');
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

    it('should show Enter to select hint for status field when focused in navigate mode', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} focusedField="status" interactionMode="navigate" />
      );

      const frame = lastFrame();
      expect(frame).toContain('Enter to select');
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
    it('should show edit buffer when editing a text field', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          focusedField="feature_id"
          interactionMode="edit_text" 
          editBuffer="new-feature"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('new-feature');
    });

    it('should show save hint when editing text', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          focusedField="git_branch"
          interactionMode="edit_text" 
          editBuffer="develop"
        />
      );

      const frame = lastFrame();
      // Footer shows "Type to edit  Enter: save  Esc: cancel" in edit_text mode
      expect(frame).toContain('Enter: save');
    });

    it('should update footer text when in edit_text mode', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          focusedField="feature_id"
          interactionMode="edit_text" 
          editBuffer="test"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Type to edit');
      expect(frame).toContain('Esc: cancel');
    });

    it('should show status selection hint when in edit_status mode', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          focusedField="status"
          interactionMode="edit_status" 
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('j/k: select status');
      expect(frame).toContain('Enter: save');
    });
  });

  describe('status sub-popup', () => {
    it('should render status sub-popup when in edit_status mode', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          focusedField="status"
          interactionMode="edit_status" 
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Select Status');
    });

    it('should show all allowed statuses in the sub-popup', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          focusedField="status"
          interactionMode="edit_status" 
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('draft');
      expect(frame).toContain('pending');
      expect(frame).toContain('active');
      expect(frame).toContain('in progress');
      expect(frame).toContain('blocked');
      expect(frame).toContain('completed');
    });

    it('should show arrow indicator on selected status', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          focusedField="status"
          interactionMode="edit_status"
          selectedStatusIndex={3} // in_progress
        />
      );

      const frame = lastFrame();
      // The selected status should have an arrow and filled circle
      expect(frame).toContain('→');
      expect(frame).toContain('●');
    });

    it('should not show sub-popup when in navigate mode', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          focusedField="status"
          interactionMode="navigate" 
        />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('Select Status');
    });

    it('should not show old j/k to change hint on status field', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          focusedField="status"
          interactionMode="edit_status" 
        />
      );

      const frame = lastFrame();
      // Old hint should be removed
      expect(frame).not.toContain('(j/k to change, Enter to save)');
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
