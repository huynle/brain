/**
 * TaskDetail Component Tests
 *
 * Tests the task detail panel showing selected task information
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { TaskDetail } from './TaskDetail';
import type { TaskDisplay } from '../types';

// Helper to create mock tasks
function createTask(overrides: Partial<TaskDisplay> = {}): TaskDisplay {
  return {
    id: 'task-123',
    title: 'Test Task',
    status: 'pending',
    priority: 'medium',
    dependencies: [],
    dependents: [],
    ...overrides,
  };
}

describe('TaskDetail', () => {
  describe('null task state', () => {
    it('shows placeholder when no task selected', () => {
      const { lastFrame } = render(<TaskDetail task={null} />);
      expect(lastFrame()).toContain('Select a task to view details');
    });
  });

  describe('task information display', () => {
    it('displays task title', () => {
      const task = createTask({ title: 'Setup Database' });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('Setup Database');
    });

    it('displays task status', () => {
      const task = createTask({ status: 'in_progress' });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('Status:');
      expect(lastFrame()).toContain('in_progress');
    });

    it('displays task priority', () => {
      const task = createTask({ priority: 'high' });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('Priority:');
      expect(lastFrame()).toContain('high');
    });

    it('displays task ID', () => {
      const task = createTask({ id: 'abc-123-def' });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('ID:');
      expect(lastFrame()).toContain('abc-123-def');
    });
  });

  describe('status colors', () => {
    it('renders pending status', () => {
      const task = createTask({ status: 'pending' });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('pending');
    });

    it('renders in_progress status', () => {
      const task = createTask({ status: 'in_progress' });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('in_progress');
    });

    it('renders completed status', () => {
      const task = createTask({ status: 'completed' });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('completed');
    });

    it('renders blocked status', () => {
      const task = createTask({ status: 'blocked' });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('blocked');
    });
  });

  describe('dependencies display', () => {
    it('shows dependencies section when task has dependencies', () => {
      const task = createTask({ dependencies: ['dep-1', 'dep-2'] });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('Dependencies:');
      expect(lastFrame()).toContain('dep-1');
      expect(lastFrame()).toContain('dep-2');
    });

    it('hides dependencies section when no dependencies', () => {
      const task = createTask({ dependencies: [] });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).not.toContain('Dependencies:');
    });
  });

  describe('error display', () => {
    it('shows error when task has error', () => {
      const task = createTask({ error: 'Failed to connect to database' });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('Error:');
      expect(lastFrame()).toContain('Failed to connect to database');
    });

    it('hides error section when no error', () => {
      const task = createTask({ error: undefined });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).not.toContain('Error:');
    });
  });

  describe('progress display', () => {
    it('shows progress when defined', () => {
      const task = createTask({ progress: 75 });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('Progress:');
      expect(lastFrame()).toContain('75%');
    });

    it('shows 0% progress', () => {
      const task = createTask({ progress: 0 });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).toContain('Progress:');
      expect(lastFrame()).toContain('0%');
    });

    it('hides progress when undefined', () => {
      const task = createTask({ progress: undefined });
      const { lastFrame } = render(<TaskDetail task={task} />);
      expect(lastFrame()).not.toContain('Progress:');
    });
  });
});
