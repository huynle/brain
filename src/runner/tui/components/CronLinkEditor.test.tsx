import React from 'react';
import { describe, expect, it } from 'bun:test';
import { render } from 'ink-testing-library';
import { CronLinkEditor } from './CronLinkEditor';
import type { CronDisplay, TaskDisplay } from '../types';

const cron: CronDisplay = {
  id: 'crn00001',
  path: 'projects/test-project/cron/crn00001.md',
  title: 'Nightly Build',
  status: 'active',
  schedule: '0 2 * * *',
  next_run: undefined,
  runs: [],
};

function makeTask(id: string, title: string, cronIds: string[] = []): TaskDisplay {
  return {
    id,
    path: `projects/test-project/task/${id}.md`,
    title,
    status: 'pending',
    priority: 'medium',
    dependencies: [],
    dependents: [],
    dependencyTitles: [],
    dependentTitles: [],
    tags: [],
    cron_ids: cronIds,
  };
}

describe('CronLinkEditor', () => {
  it('renders counts and editor hints for linked task selection', () => {
    const tasks: TaskDisplay[] = [
      makeTask('task-1', 'Task One', ['crn00001']),
      makeTask('task-2', 'Task Two'),
    ];

    const { lastFrame, unmount } = render(
      <CronLinkEditor
        cron={cron}
        projectId="test-project"
        tasks={tasks}
        linkedTaskIds={new Set(['task-1'])}
        selectedIndex={0}
      />
    );

    const frame = lastFrame() || '';
    expect(frame).toContain('Edit Cron Linked Tasks');
    expect(frame).toContain('Nightly Build');
    expect(frame).toContain('Linked:');
    expect(frame).toContain('Available:');
    expect(frame).toContain('j/k move, Space toggle, Enter apply, Esc cancel');

    unmount();
  });

  it('shows empty state when no project tasks are available', () => {
    const { lastFrame, unmount } = render(
      <CronLinkEditor
        cron={cron}
        projectId="test-project"
        tasks={[]}
        linkedTaskIds={new Set()}
        selectedIndex={0}
      />
    );

    expect(lastFrame() || '').toContain('(no tasks available in this project)');

    unmount();
  });

  it('truncates long task titles and shows overflow indicator', () => {
    const tasks: TaskDisplay[] = [
      makeTask('task-1', 'This is a very long task title that should be truncated in the editor row rendering'),
      ...Array.from({ length: 12 }, (_, index) => makeTask(`task-${index + 2}`, `Task ${index + 2}`)),
    ];

    const { lastFrame, unmount } = render(
      <CronLinkEditor
        cron={cron}
        projectId="test-project"
        tasks={tasks}
        linkedTaskIds={new Set()}
        selectedIndex={0}
      />
    );

    const frame = lastFrame() || '';
    expect(frame).toContain('This is a very long task title that shoul...');
    expect(frame).toContain('...and 1 more');

    unmount();
  });
});
