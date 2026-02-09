/**
 * Tests for the PausePopup component
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { PausePopup } from './PausePopup';

describe('PausePopup', () => {
  describe('rendering', () => {
    it('should render the header', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          selectedTarget="project"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Pause/Resume');
    });

    it('should render project option with project ID', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="brain-api"
          selectedTarget="project"
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
          selectedTarget="project"
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
          selectedTarget="project"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Enter');
      expect(frame).toContain('Toggle');
      expect(frame).toContain('Esc');
      expect(frame).toContain('Cancel');
    });
  });

  describe('with feature', () => {
    it('should render feature option when featureId is provided', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          featureId="auth-system"
          selectedTarget="project"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Feature: auth-system');
    });

    it('should show navigation shortcuts when feature is available', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          featureId="auth-system"
          selectedTarget="project"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('j/k');
      expect(frame).toContain('Navigate');
    });

    it('should truncate long feature IDs', () => {
      const longFeatureId = 'this-is-a-very-long-feature-name-exceeding-limit';
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          featureId={longFeatureId}
          selectedTarget="project"
        />
      );

      const frame = lastFrame();
      // Feature ID should be truncated with ellipsis
      expect(frame).toContain('Feature:');
      expect(frame).toContain('...');
      expect(frame).not.toContain(longFeatureId);
    });
  });

  describe('without feature', () => {
    it('should only show project option when no featureId', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          selectedTarget="project"
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('Project: my-project');
      expect(frame).not.toContain('Feature:');
    });

    it('should not show j/k navigation when no feature', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          selectedTarget="project"
        />
      );

      const frame = lastFrame();
      expect(frame).not.toContain('j/k');
    });
  });

  describe('selection indication', () => {
    it('should show arrow indicator for selected project', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          featureId="my-feature"
          selectedTarget="project"
        />
      );

      const frame = lastFrame();
      // The selection arrow should appear
      expect(frame).toContain('→');
    });

    it('should show arrow indicator for selected feature', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          featureId="my-feature"
          selectedTarget="feature"
        />
      );

      const frame = lastFrame();
      // Arrow should be next to feature
      expect(frame).toContain('→');
    });
  });

  describe('pause state indication', () => {
    it('should show running state when not paused', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          selectedTarget="project"
          isProjectPaused={false}
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
          selectedTarget="project"
          isProjectPaused={true}
        />
      );

      const frame = lastFrame();
      expect(frame).toContain('(paused)');
      expect(frame).toContain('⏸');
    });

    it('should show independent pause states for project and feature', () => {
      const { lastFrame } = render(
        <PausePopup
          projectId="my-project"
          featureId="my-feature"
          selectedTarget="project"
          isProjectPaused={false}
          isFeaturePaused={true}
        />
      );

      const frame = lastFrame();
      // Should show both states
      expect(frame).toContain('(running)');
      expect(frame).toContain('(paused)');
    });
  });
});
