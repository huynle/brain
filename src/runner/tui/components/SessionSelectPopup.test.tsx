/**
 * Tests for the SessionSelectPopup component
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { SessionSelectPopup } from './SessionSelectPopup';

describe('SessionSelectPopup', () => {
  describe('rendering', () => {
    it('should render the header', () => {
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={['ses_abc123']}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Select Session');
    });

    it('should show singular session count', () => {
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={['ses_abc123']}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('1 session');
      // Should not have 's' for plural
      expect(frame).not.toContain('1 sessions');
    });

    it('should show plural session count', () => {
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={['ses_abc123', 'ses_def456', 'ses_ghi789']}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('3 sessions');
    });

    it('should display session IDs', () => {
      const sessionIds = ['ses_abc123', 'ses_def456', 'ses_ghi789'];
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={sessionIds}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('ses_abc123');
      expect(frame).toContain('ses_def456');
      expect(frame).toContain('ses_ghi789');
    });

    it('should show (latest) label on first session', () => {
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={['ses_abc123', 'ses_def456']}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('(latest)');
    });

    it('should show selection indicator on selected session', () => {
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={['ses_abc123', 'ses_def456', 'ses_ghi789']}
          selectedIndex={1}
        />
      );

      const frame = lastFrame();
      // The selected session should have '>' prefix
      // Note: We check for the pattern, exact rendering depends on Ink
      expect(frame).toContain('> ses_def456');
    });

    it('should show keyboard shortcuts', () => {
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={['ses_abc123']}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('j/k: Navigate');
      expect(frame).toContain('Enter: Open');
      expect(frame).toContain('Esc: Cancel');
    });
  });

  describe('truncation', () => {
    it('should truncate when more than 8 sessions', () => {
      const sessionIds = [
        'ses_1', 'ses_2', 'ses_3', 'ses_4',
        'ses_5', 'ses_6', 'ses_7', 'ses_8',
        'ses_9', 'ses_10'
      ];
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={sessionIds}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      // First 8 should be visible
      expect(frame).toContain('ses_1');
      expect(frame).toContain('ses_8');
      // Last 2 should be hidden
      expect(frame).not.toContain('ses_9');
      expect(frame).not.toContain('ses_10');
      // Should show "and 2 more"
      expect(frame).toContain('and 2 more');
    });

    it('should truncate long session IDs', () => {
      const longSessionId = 'ses_this_is_a_very_long_session_id_that_exceeds_max';
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={[longSessionId]}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('...');
      expect(frame).not.toContain(longSessionId);
    });
  });

  describe('selection', () => {
    it('should highlight first session when selectedIndex is 0', () => {
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={['ses_abc123', 'ses_def456']}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('> ses_abc123');
    });

    it('should highlight second session when selectedIndex is 1', () => {
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={['ses_abc123', 'ses_def456']}
          selectedIndex={1}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('> ses_def456');
    });
  });

  describe('edge cases', () => {
    it('should handle empty session list', () => {
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={[]}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Select Session');
      expect(frame).toContain('0 sessions');
    });

    it('should handle exactly 8 sessions without truncation message', () => {
      const sessionIds = [
        'ses_1', 'ses_2', 'ses_3', 'ses_4',
        'ses_5', 'ses_6', 'ses_7', 'ses_8'
      ];
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={sessionIds}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('ses_1');
      expect(frame).toContain('ses_8');
      expect(frame).not.toContain('more');
    });

    it('should handle single session', () => {
      const { lastFrame } = render(
        <SessionSelectPopup
          sessionIds={['ses_single']}
          selectedIndex={0}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('> ses_single');
      expect(frame).toContain('(latest)');
      expect(frame).toContain('1 session');
    });
  });
});
