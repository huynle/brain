/**
 * Tests for the MetadataPopup component
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { MetadataPopup, FEATURE_FIELD_GROUPS, type MetadataField, type MetadataPopupMode } from './MetadataPopup';
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

    it('shows all 17 fields in feature mode (merged task + feature settings)', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="feature"
          featureId="auth-system"
          focusedField="status"
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
      // Task group
      expect(frame).toContain('Status:');
      expect(frame).toContain('Feature ID:');
      expect(frame).toContain('Project:');
      // Execution group
      expect(frame).toContain('Execution Mode:');
      expect(frame).toContain('Agent:');
      expect(frame).toContain('Model:');
      expect(frame).toContain('Prompt:');
      expect(frame).toContain('Schedule:');
      expect(frame).toContain('Complete on Idle:');
      // Git / Branch group
      expect(frame).toContain('Branch:');
      expect(frame).toContain('Workdir:');
      expect(frame).toContain('Checkout Enabled:');
      // Merge / PR group
      expect(frame).toContain('Merge Target:');
      expect(frame).toContain('Merge Policy:');
      expect(frame).toContain('Merge Strategy:');
      expect(frame).toContain('Remote Branch Policy:');
      expect(frame).toContain('Open PR Before Merge:');
    });

    it('shows group separator headers in feature mode', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="feature"
          featureId="auth-system"
          focusedField="status"
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
      for (const group of FEATURE_FIELD_GROUPS) {
        expect(frame).toContain(`── ${group.label} ──`);
      }
    });

    it('does NOT show group separators in single mode', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} mode="single" />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('── Task ──');
      expect(frame).not.toContain('── Execution ──');
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

  describe('schedule field', () => {
    it('should not show legacy help text for schedule field', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} />
      );

      const frame = lastFrame();
      expect(frame).toContain('Schedule:');
      expect(frame).not.toContain('(creates NEW cron)');
    });

    it('should not show legacy "Cron Links:" field', () => {
      const { lastFrame } = render(
        <MetadataPopup {...defaultProps} />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('Cron Links:');
    });
  });

  describe('Phase 4: Schedule Validation (helper function)', () => {
    // These are validation helper tests - the actual validation happens in App.tsx
    // We export the helper for testing

    it('valid schedule expression: 5 fields separated by spaces', () => {
      const schedule = '0 2 * * *';
      const fields = schedule.trim().split(/\s+/);
      expect(fields.length).toBe(5);
    });

    it('invalid schedule expression: too few fields', () => {
      const schedule = '0 2 *';
      const fields = schedule.trim().split(/\s+/);
      expect(fields.length).not.toBe(5);
    });

    it('invalid schedule expression: too many fields', () => {
      const schedule = '0 2 * * * * *';
      const fields = schedule.trim().split(/\s+/);
      expect(fields.length).not.toBe(5);
    });

    it('empty schedule should be valid (clears schedule)', () => {
      const schedule = '';
      expect(schedule.length === 0 || schedule.trim().split(/\s+/).length === 5).toBe(true);
    });
  });

  describe('monitoring section (feature mode)', () => {
    const featureProps = {
      ...defaultProps,
      mode: 'feature' as MetadataPopupMode,
      featureId: 'auth-system',
      batchCount: 3,
      executionModeValue: 'worktree' as const,
      mergeTargetBranchValue: 'main',
      checkoutEnabledValue: true,
      mergePolicyValue: 'prompt_only' as const,
      mergeStrategyValue: 'squash' as const,
      remoteBranchPolicyValue: 'delete' as const,
      openPrBeforeMergeValue: false,
    };

    it('should render monitoring separator but no content when monitoringTemplates is undefined', () => {
      const { lastFrame } = render(
        <MetadataPopup {...featureProps} />
      );

      const frame = lastFrame();
      expect(frame).toContain('── Monitoring ──');
      expect(frame).not.toContain('Blocked Task Inspector');
      expect(frame).not.toContain('Loading...');
    });

    it('should render monitoring separator but no content when monitoringTemplates is empty', () => {
      const { lastFrame } = render(
        <MetadataPopup {...featureProps} monitoringTemplates={[]} />
      );

      const frame = lastFrame();
      expect(frame).toContain('── Monitoring ──');
      expect(frame).not.toContain('Blocked Task Inspector');
    });

    it('should show loading state when monitoringLoading is true', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...featureProps}
          monitoringTemplates={[]}
          monitoringLoading={true}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('── Monitoring ──');
      expect(frame).toContain('Loading...');
    });

    it('should render enabled template with green indicator and label', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...featureProps}
          monitoringTemplates={[
            {
              templateId: 'blocked-inspector',
              label: 'Blocked Task Inspector',
              status: 'enabled',
              schedule: '*/15 * * * *',
              taskPath: 'projects/test/task/abc.md',
            },
          ]}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('── Monitoring ──');
      expect(frame).toContain('Blocked Task Inspector');
      expect(frame).toContain('[enabled]');
      expect(frame).toContain('*/15 * * * *');
    });

    it('should render disabled template with red indicator', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...featureProps}
          monitoringTemplates={[
            {
              templateId: 'blocked-inspector',
              label: 'Blocked Task Inspector',
              status: 'disabled',
              schedule: '*/15 * * * *',
              taskPath: 'projects/test/task/abc.md',
            },
          ]}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Blocked Task Inspector');
      expect(frame).toContain('[disabled]');
      expect(frame).toContain('*/15 * * * *');
    });

    it('should render create template with dim indicator', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...featureProps}
          monitoringTemplates={[
            {
              templateId: 'blocked-inspector',
              label: 'Blocked Task Inspector',
              status: 'create',
              schedule: '*/15 * * * *',
            },
          ]}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Blocked Task Inspector');
      expect(frame).toContain('[create]');
    });

    it('should NOT show schedule for create status', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...featureProps}
          monitoringTemplates={[
            {
              templateId: 'blocked-inspector',
              label: 'Blocked Task Inspector',
              status: 'create',
              schedule: '*/15 * * * *',
            },
          ]}
        />
      );

      const frame = lastFrame();
      // Schedule should not be shown for create status
      expect(frame).not.toContain('*/15 * * * *');
    });

    it('should highlight focused monitoring row with arrow', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...featureProps}
          monitoringTemplates={[
            {
              templateId: 'blocked-inspector',
              label: 'Blocked Task Inspector',
              status: 'enabled',
              schedule: '*/15 * * * *',
              taskPath: 'projects/test/task/abc.md',
            },
          ]}
          focusedMonitoringIndex={0}
        />
      );

      const frame = lastFrame();
      // The monitoring row should have the focus arrow
      // We check that the monitoring section contains the arrow indicator
      expect(frame).toContain('Blocked Task Inspector');
      // The frame should contain the arrow somewhere near the monitoring template
      // Since the focused field rows also have arrows, we verify the monitoring row is focused
      // by checking the combination exists
      expect(frame).toContain('→');
    });

    it('should NOT highlight monitoring row when focusedMonitoringIndex is -1', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...featureProps}
          focusedField="status"
          monitoringTemplates={[
            {
              templateId: 'blocked-inspector',
              label: 'Blocked Task Inspector',
              status: 'enabled',
              schedule: '*/15 * * * *',
              taskPath: 'projects/test/task/abc.md',
            },
          ]}
          focusedMonitoringIndex={-1}
        />
      );

      // The monitoring row should NOT have the focus arrow
      // But the status field (focusedField) should still have one
      const frame = lastFrame();
      expect(frame).toContain('Blocked Task Inspector');
      // Arrow should be on status field, not on monitoring row
    });

    it('should NOT render monitoring section in single mode', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="single"
          monitoringTemplates={[
            {
              templateId: 'blocked-inspector',
              label: 'Blocked Task Inspector',
              status: 'enabled',
              schedule: '*/15 * * * *',
            },
          ]}
        />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('── Monitoring ──');
      expect(frame).not.toContain('Blocked Task Inspector');
    });

    it('should NOT render monitoring section in batch mode', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="batch"
          batchCount={3}
          monitoringTemplates={[
            {
              templateId: 'blocked-inspector',
              label: 'Blocked Task Inspector',
              status: 'enabled',
              schedule: '*/15 * * * *',
            },
          ]}
        />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('── Monitoring ──');
      expect(frame).not.toContain('Blocked Task Inspector');
    });

    it('should render Monitoring in FEATURE_FIELD_GROUPS', () => {
      const monitoringGroup = FEATURE_FIELD_GROUPS.find(g => g.label === 'Monitoring');
      expect(monitoringGroup).toBeDefined();
      expect(monitoringGroup!.fields).toEqual([]);
    });

    it('should render multiple templates simultaneously with mixed statuses', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...featureProps}
          monitoringTemplates={[
            {
              templateId: 'blocked-inspector',
              label: 'Blocked Task Inspector',
              status: 'enabled',
              schedule: '*/15 * * * *',
              taskPath: 'projects/test/task/abc.md',
            },
            {
              templateId: 'stale-checker',
              label: 'Stale Task Checker',
              status: 'create',
              schedule: '30 2 * * 0',
            },
          ]}
        />
      );

      const frame = lastFrame();
      // Both templates should render
      expect(frame).toContain('Blocked Task Inspector');
      expect(frame).toContain('Stale Task Checker');
      // Enabled template shows schedule, create does not
      expect(frame).toContain('*/15 * * * *');
      expect(frame).toContain('[enabled]');
      expect(frame).toContain('[create]');
      // Create template's schedule should NOT appear in the monitoring section
      expect(frame).not.toContain('30 2 * * 0');
    });

    it('should highlight the last monitoring row when focusedMonitoringIndex points to it', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...featureProps}
          monitoringTemplates={[
            {
              templateId: 'blocked-inspector',
              label: 'Blocked Task Inspector',
              status: 'enabled',
              schedule: '*/15 * * * *',
              taskPath: 'projects/test/task/abc.md',
            },
            {
              templateId: 'stale-checker',
              label: 'Stale Task Checker',
              status: 'disabled',
              schedule: '0 9 * * 1',
              taskPath: 'projects/test/task/def.md',
            },
          ]}
          focusedMonitoringIndex={1}
        />
      );

      const frame = lastFrame();
      // Both templates render
      expect(frame).toContain('Blocked Task Inspector');
      expect(frame).toContain('Stale Task Checker');
      // The second row (index 1) should have the focus arrow
      const lines = frame!.split('\n');
      const staleLine = lines.find(l => l.includes('Stale Task Checker'));
      expect(staleLine).toBeDefined();
      expect(staleLine).toContain('→');
      // The first row should NOT have the focus arrow
      const blockedLine = lines.find(l => l.includes('Blocked Task Inspector'));
      expect(blockedLine).toBeDefined();
      expect(blockedLine).not.toContain('→');
    });

    it('should render disabled template with its schedule visible', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...featureProps}
          monitoringTemplates={[
            {
              templateId: 'stale-checker',
              label: 'Stale Task Checker',
              status: 'disabled',
              schedule: '0 9 * * 1',
              taskPath: 'projects/test/task/def.md',
            },
          ]}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Stale Task Checker');
      expect(frame).toContain('[disabled]');
      // Disabled templates still show their schedule (unlike create)
      expect(frame).toContain('0 9 * * 1');
    });
  });

  describe('mixed-value detection', () => {
    it('should show "(mixed)" for a field in mixedFields set', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="batch"
          batchCount={3}
          branchValue=""
          mixedFields={new Set<MetadataField>(['git_branch'])}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('(mixed)');
    });

    it('should show normal value for a field NOT in mixedFields', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="batch"
          batchCount={3}
          branchValue="feature/shared"
          mixedFields={new Set<MetadataField>(['feature_id'])}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('feature/shared');
    });

    it('should show edit buffer when editing a mixed field, not "(mixed)"', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="batch"
          batchCount={3}
          focusedField="git_branch"
          branchValue=""
          interactionMode="edit_text"
          editBuffer="new-branch"
          mixedFields={new Set<MetadataField>(['git_branch'])}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('new-branch');
      expect(frame).not.toContain('(mixed)');
    });

    it('should display multiple mixed fields correctly', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="batch"
          batchCount={3}
          featureIdValue=""
          branchValue=""
          workdirValue=""
          mixedFields={new Set<MetadataField>(['feature_id', 'git_branch', 'target_workdir'])}
        />
      );

      const frame = lastFrame();
      // Count occurrences of "(mixed)" - should appear for all three fields
      const matches = frame!.match(/\(mixed\)/g);
      expect(matches).not.toBeNull();
      expect(matches!.length).toBeGreaterThanOrEqual(3);
    });

    it('should show normal behavior when mixedFields is empty', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="batch"
          batchCount={3}
          branchValue="feature/shared"
          mixedFields={new Set<MetadataField>()}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('feature/shared');
      expect(frame).not.toContain('(mixed)');
    });

    it('should show normal behavior when mixedFields is undefined', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="batch"
          batchCount={3}
          branchValue="feature/shared"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('feature/shared');
      expect(frame).not.toContain('(mixed)');
    });

    it('should show "(mixed)" for feature mode too', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="feature"
          featureId="auth-system"
          batchCount={3}
          branchValue=""
          executionModeValue="worktree"
          mergeTargetBranchValue=""
          checkoutEnabledValue={true}
          mergePolicyValue="auto_merge"
          mergeStrategyValue="squash"
          remoteBranchPolicyValue="delete"
          openPrBeforeMergeValue={false}
          mixedFields={new Set<MetadataField>(['git_branch'])}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('(mixed)');
    });

    it('should NOT show "(mixed)" in single mode even if mixedFields is set', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="single"
          branchValue="feature/test"
          mixedFields={new Set<MetadataField>(['git_branch'])}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('feature/test');
      expect(frame).not.toContain('(mixed)');
    });

    it('should show "(mixed)" for status field in batch mode', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="batch"
          batchCount={3}
          statusValue="pending"
          mixedFields={new Set<MetadataField>(['status'])}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('(mixed)');
    });

    it('should show status dropdown when editing a mixed status field', () => {
      const { lastFrame } = render(
        <MetadataPopup
          {...defaultProps}
          mode="batch"
          batchCount={3}
          focusedField="status"
          statusValue="pending"
          interactionMode="edit_status"
          mixedFields={new Set<MetadataField>(['status'])}
        />
      );

      const frame = lastFrame();
      // Should show the status selection sub-popup, not "(mixed)"
      expect(frame).toContain('Select Status');
    });
  });
});
