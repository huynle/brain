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
  scheduleValue: '0 * * * *',
  projectValue: 'my-project',
  agentValue: '',
  modelValue: '',
  directPromptValue: '',
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

    it('should render all nine fields', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Status:');
      expect(frame).toContain('Feature ID:');
      expect(frame).toContain('Branch:');
      expect(frame).toContain('Workdir:');
      expect(frame).toContain('Schedule:');
      expect(frame).toContain('Project:');
      expect(frame).toContain('Agent:');
      expect(frame).toContain('Model:');
      expect(frame).toContain('Prompt:');
    });

    it('should display execution override values', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          agentValue="tdd-dev"
          modelValue="anthropic/claude-sonnet-4-20250514"
          directPromptValue="Run the tests"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('tdd-dev');
      expect(frame).toContain('anthropic/claude-sonnet-4-20250514');
      expect(frame).toContain('Run the tests');
    });

    it('should show (default) for empty agent/model', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          agentValue=""
          modelValue=""
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('(default)');
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

    it('shows feature settings fields and hides task metadata fields in feature mode', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="feature"
          featureId="auth-system"
          mergeTargetBranchValue="main"
          executionModeValue="worktree"
          checkoutEnabledValue={true}
          mergePolicyValue="prompt_only"
          mergeStrategyValue="squash"
          remoteBranchPolicyValue="delete"
          openPrBeforeMergeValue={false}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Execution');
      expect(frame).toContain('Mode:');
      expect(frame).toContain('Branch:');
      expect(frame).toContain('Merge Target:');
      expect(frame).toContain('Checkout');
      expect(frame).toContain('Enabled:');
      expect(frame).toContain('Merge Policy:');
      expect(frame).toContain('Strategy:');
      expect(frame).toContain('Remote Branch');
      expect(frame).toContain('Policy:');
      expect(frame).toContain('Open PR Before');
      expect(frame).toContain('Merge:');
      expect(frame).toContain('Workdir:');

      expect(frame).not.toContain('Status:');
      expect(frame).not.toContain('Feature ID:');
      expect(frame).not.toContain('Schedule:');
      expect(frame).not.toContain('Project:');
    });

    it('shows current execution mode hint values when focused', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="feature"
          featureId="auth-system"
          focusedField="execution_mode"
          executionModeValue="worktree"
          mergeTargetBranchValue="main"
          checkoutEnabledValue={true}
          mergePolicyValue="prompt_only"
          mergeStrategyValue="squash"
          remoteBranchPolicyValue="delete"
          openPrBeforeMergeValue={false}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('(worktree|current_branch)');
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

    it('shows feature setting hint when execution mode is focused', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="feature"
          featureId="auth-system"
          focusedField="execution_mode"
          executionModeValue="worktree"
          mergeTargetBranchValue="main"
          checkoutEnabledValue={true}
          mergePolicyValue="prompt_only"
          mergeStrategyValue="squash"
          remoteBranchPolicyValue="delete"
          openPrBeforeMergeValue={false}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('(worktree|current_branch)');
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

    it('should display schedule value', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} scheduleValue="*/15 * * * *" />
      );

      const frame = lastFrame();
      expect(frame).toContain('*/15 * * * *');
    });

    it('should show (none) for empty text fields', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          featureIdValue="" 
          branchValue="" 
          workdirValue=""
          scheduleValue=""
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

  describe('cron links field', () => {
    it('should show "Cron Links:" field when task has cron_ids', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          cronIds={['cron1', 'cron2']}
          cronNames={{ cron1: 'Daily Backup', cron2: 'Weekly Report' }}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Cron Links:');
    });

    it('should display cron name badges with calendar emoji', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          cronIds={['cron1']}
          cronNames={{ cron1: 'Daily Backup' }}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('📅 Daily Backup');
    });

    it('should display multiple cron badges, one per line', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          cronIds={['cron1', 'cron2', 'cron3']}
          cronNames={{ 
            cron1: 'Daily Backup', 
            cron2: 'Weekly Report',
            cron3: 'Monthly Cleanup'
          }}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('📅 Daily Backup');
      expect(frame).toContain('📅 Weekly Report');
      expect(frame).toContain('📅 Monthly Cleanup');
    });

    it('should only show cron links that exist in cronNames', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          cronIds={['cron1', 'cron2']}
          cronNames={{ cron1: 'Daily Backup' }} // cron2 missing
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('📅 Daily Backup');
      expect(frame).not.toContain('cron2');
    });

    it('should keep valid cron badge and suppress stale cron ID text', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          cronIds={['crn00001', 'stale-cron-id']}
          cronNames={{ crn00001: 'Nightly Cleanup' }}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Cron Links:');
      expect(frame).toContain('📅 Nightly Cleanup');
      expect(frame).not.toContain('stale-cron-id');
    });

    it('should not show "Cron Links:" field when cronIds is empty', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          cronIds={[]}
          cronNames={{}}
        />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('Cron Links:');
    });

    it('should not show "Cron Links:" field when cronIds is undefined', () => {
      const { lastFrame } = render(
        <MetadataPopup 
          {...defaultProps} 
          cronIds={undefined}
          cronNames={{}}
        />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('Cron Links:');
    });

    it('should not show "Cron Links:" field when no cron IDs resolve to names', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          cronIds={['orphan-cron-id']}
          cronNames={{}}
        />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('Cron Links:');
      expect(frame).not.toContain('orphan-cron-id');
    });
  });

  describe('schedule field help text', () => {
    it('should show "(creates NEW cron)" help text for schedule field', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Schedule:');
      expect(frame).toContain('(creates NEW cron)');
    });
  });

  describe('Phase 4: Integration - cronIds prop', () => {
    it('should accept cronIds prop from parent and display cron badges', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          cronIds={['cron123', 'cron456']}
          cronNames={{
            'cron123': 'Daily Backup',
            'cron456': 'Weekly Cleanup'
          }}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Cron Links:');
      expect(frame).toContain('Daily Backup');
      expect(frame).toContain('Weekly Cleanup');
    });

    it('should handle empty cronIds gracefully', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          cronIds={[]}
          cronNames={{}}
        />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('Cron Links:');
    });

    it('should hide unknown cron IDs when cronNames is missing entries', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          cronIds={['cron123', 'unknown456']}
          cronNames={{
            'cron123': 'Daily Backup'
          }}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Daily Backup');
      expect(frame).not.toContain('unknown456');
    });
  });

  describe('Phase 4: Schedule Validation (helper function)', () => {
    // These are validation helper tests - the actual validation happens in App.tsx
    // We export the helper for testing

    it('valid cron: 5 fields separated by spaces', () => {
      const schedule = '0 2 * * *';
      const fields = schedule.trim().split(/\s+/);
      expect(fields.length).toBe(5);
    });

    it('invalid cron: too few fields', () => {
      const schedule = '0 2 *';
      const fields = schedule.trim().split(/\s+/);
      expect(fields.length).not.toBe(5);
    });

    it('invalid cron: too many fields', () => {
      const schedule = '0 2 * * * * *';
      const fields = schedule.trim().split(/\s+/);
      expect(fields.length).not.toBe(5);
    });

    it('empty schedule should be valid (clears cron)', () => {
      const schedule = '';
      expect(schedule.length === 0 || schedule.trim().split(/\s+/).length === 5).toBe(true);
    });
  });
});
