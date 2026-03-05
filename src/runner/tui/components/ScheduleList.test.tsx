import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { ScheduleList } from './ScheduleList';
import type { TaskDisplay } from '../types';

function createScheduledTask(overrides: Partial<TaskDisplay> = {}): TaskDisplay {
  return {
    id: 'task-1',
    path: 'projects/test/task/task-1.md',
    title: 'Nightly cleanup',
    status: 'active',
    priority: 'medium',
    tags: [],
    schedule: '0 2 * * *',
    dependencies: [],
    dependents: [],
    dependencyTitles: [],
    dependentTitles: [],
    projectId: 'test',
    ...overrides,
  };
}

describe('ScheduleList', () => {
  it('shows empty state when no scheduled tasks exist', () => {
    const { lastFrame } = render(
      <ScheduleList tasks={[]} selectedId={null} />
    );
    expect(lastFrame()).toContain('No scheduled tasks found');
  });

  it('renders scheduled tasks and highlights selected row', () => {
    const tasks = [
      createScheduledTask({ id: 'task-1', title: 'Nightly cleanup' }),
      createScheduledTask({ id: 'task-2', title: 'Hourly sync', schedule: '0 * * * *' }),
    ];
    const { lastFrame } = render(
      <ScheduleList tasks={tasks} selectedId="task-2" isFocused />
    );

    const frame = lastFrame() || '';
    expect(frame).toContain('Scheduled');
    expect(frame).toContain('Nightly cleanup');
    expect(frame).toContain('Hourly sync');
    expect(frame).toContain('>');
  });

  it('shows project prefix in multi-project aggregate mode', () => {
    const { lastFrame } = render(
      <ScheduleList
        tasks={[createScheduledTask({ projectId: 'brain-api' })]}
        selectedId="task-1"
        showProjectPrefix
      />
    );

    expect(lastFrame()).toContain('[brain-api]');
  });

  it('shows schedule expression and scheduled badge', () => {
    const { lastFrame } = render(
      <ScheduleList
        tasks={[
          createScheduledTask({
            schedule: '*/5 * * * *',
            priority: 'high',
            tags: ['deploy'],
          }),
        ]}
        selectedId="task-1"
      />
    );

    const frame = lastFrame() || '';
    expect(frame).toContain('*/5 * * * *');
    expect(frame).toContain('[scheduled]');
  });
});
