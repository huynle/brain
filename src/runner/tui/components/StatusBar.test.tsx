/**
 * StatusBar Component Tests
 *
 * Tests the top status bar showing task counts and connection status
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { StatusBar } from './StatusBar';

describe('StatusBar', () => {
  const defaultStats = {
    ready: 2,
    waiting: 3,
    inProgress: 1,
    completed: 5,
    blocked: 0,
  };

  describe('task counts display', () => {
    it('shows correct ready count', () => {
      const { lastFrame } = render(
        <StatusBar
          projectId="test-project"
          stats={defaultStats}
          isConnected={true}
        />
      );
      expect(lastFrame()).toContain('● 2 ready');
    });

    it('shows correct waiting count', () => {
      const { lastFrame } = render(
        <StatusBar
          projectId="test-project"
          stats={defaultStats}
          isConnected={true}
        />
      );
      expect(lastFrame()).toContain('○ 3 waiting');
    });

    it('shows correct active (in progress) count', () => {
      const { lastFrame } = render(
        <StatusBar
          projectId="test-project"
          stats={defaultStats}
          isConnected={true}
        />
      );
      expect(lastFrame()).toContain('▶ 1 active');
    });

    it('shows correct completed count', () => {
      const { lastFrame } = render(
        <StatusBar
          projectId="test-project"
          stats={defaultStats}
          isConnected={true}
        />
      );
      expect(lastFrame()).toContain('✓ 5 done');
    });

    it('hides blocked count when 0', () => {
      const { lastFrame } = render(
        <StatusBar
          projectId="test-project"
          stats={{ ...defaultStats, blocked: 0 }}
          isConnected={true}
        />
      );
      expect(lastFrame()).not.toContain('blocked');
    });

    it('shows blocked count when > 0', () => {
      const { lastFrame } = render(
        <StatusBar
          projectId="test-project"
          stats={{ ...defaultStats, blocked: 2 }}
          isConnected={true}
        />
      );
      expect(lastFrame()).toContain('✗ 2 blocked');
    });
  });

  describe('connection status', () => {
    it('shows online when connected', () => {
      const { lastFrame } = render(
        <StatusBar
          projectId="test-project"
          stats={defaultStats}
          isConnected={true}
        />
      );
      expect(lastFrame()).toContain('● online');
    });

    it('shows offline when disconnected', () => {
      const { lastFrame } = render(
        <StatusBar
          projectId="test-project"
          stats={defaultStats}
          isConnected={false}
        />
      );
      expect(lastFrame()).toContain('○ offline');
    });
  });

  describe('project name', () => {
    it('displays the project name', () => {
      const { lastFrame } = render(
        <StatusBar
          projectId="my-awesome-project"
          stats={defaultStats}
          isConnected={true}
        />
      );
      expect(lastFrame()).toContain('my-awesome-project');
    });

    it('handles empty project name', () => {
      const { lastFrame } = render(
        <StatusBar projectId="" stats={defaultStats} isConnected={true} />
      );
      // Should still render without crashing
      expect(lastFrame()).toBeDefined();
    });
  });

  describe('edge cases', () => {
    it('handles all zeros', () => {
      const { lastFrame } = render(
        <StatusBar
          projectId="test"
          stats={{
            ready: 0,
            waiting: 0,
            inProgress: 0,
            completed: 0,
            blocked: 0,
          }}
          isConnected={true}
        />
      );
      expect(lastFrame()).toContain('● 0 ready');
      expect(lastFrame()).toContain('○ 0 waiting');
      expect(lastFrame()).toContain('▶ 0 active');
      expect(lastFrame()).toContain('✓ 0 done');
    });

    it('handles large numbers', () => {
      const { lastFrame } = render(
        <StatusBar
          projectId="test"
          stats={{
            ready: 999,
            waiting: 1000,
            inProgress: 50,
            completed: 5000,
            blocked: 100,
          }}
          isConnected={true}
        />
      );
      expect(lastFrame()).toContain('● 999 ready');
      expect(lastFrame()).toContain('○ 1000 waiting');
      expect(lastFrame()).toContain('✗ 100 blocked');
    });
  });
});
