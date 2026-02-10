/**
 * Tests for the PausePopup component
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { PausePopup } from './PausePopup';

const noop = () => {};

describe('PausePopup', () => {
  describe('rendering', () => {
    it('should render the header', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          onPauseProject={noop}
          onUnpauseProject={noop}
          onClose={noop}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Pause/Resume');
    });

    it('should render project with project ID', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="brain-api"
          onPauseProject={noop}
          onUnpauseProject={noop}
          onClose={noop}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Project: brain-api');
    });

    it('should truncate long project IDs', () => {
      const longProjectId = 'this-is-a-very-long-project-name-that-exceeds-limit';
      const { lastFrame } = render(
        <PausePopup
          projectId={longProjectId}
          onPauseProject={noop}
          onUnpauseProject={noop}
          onClose={noop}
        />
      );

      const frame = lastFrame();
      // Should be truncated with ellipsis
      expect(frame).toContain('...');
      expect(frame).not.toContain(longProjectId);
    });

    it('should show keyboard shortcuts in footer', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          onPauseProject={noop}
          onUnpauseProject={noop}
          onClose={noop}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Enter');
      expect(frame).toContain('Toggle');
      expect(frame).toContain('Esc');
      expect(frame).toContain('Cancel');
    });
  });

  describe('pause state indication', () => {
    it('should show running state when not paused', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          isProjectPaused={false}
          onPauseProject={noop}
          onUnpauseProject={noop}
          onClose={noop}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('(running)');
      expect(frame).toContain('▶');
    });

    it('should show paused state when project is paused', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          isProjectPaused={true}
          onPauseProject={noop}
          onUnpauseProject={noop}
          onClose={noop}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('(paused)');
      expect(frame).toContain('⏸');
    });
  });

  describe('focus mode indicator', () => {
    it('should show focus mode when focusedFeature is set', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          isProjectPaused={true}
          focusedFeature="auth-system"
          onPauseProject={noop}
          onUnpauseProject={noop}
          onClose={noop}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Focus: auth-system');
    });

    it('should not show focus mode indicator when focusedFeature is null', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          isProjectPaused={true}
          focusedFeature={null}
          onPauseProject={noop}
          onUnpauseProject={noop}
          onClose={noop}
        />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('Focus:');
    });
  });

  describe('selection indicator', () => {
    it('should show selection arrow', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          onPauseProject={noop}
          onUnpauseProject={noop}
          onClose={noop}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('→');
    });
  });
});
