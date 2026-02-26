import { describe, expect, it } from 'bun:test';
import {
  COMPLETED_FEATURE_PREFIX,
  FEATURE_HEADER_PREFIX,
  parseTaskTreeRowTarget,
} from './components/TaskTree';

type CheckoutResult = {
  created: boolean;
  taskId: string;
  taskTitle: string;
};

function isCheckoutEligibleRow(rowId: string): boolean {
  const target = parseTaskTreeRowTarget(rowId);
  return target.kind === 'feature_header' && Boolean(target.featureId);
}

function formatCheckoutSuccessMessage(result: CheckoutResult): string {
  return `${result.created ? 'Created' : 'Reused'} checkout task: ${result.taskId} - ${result.taskTitle}`;
}

describe('App feature checkout row gating', () => {
  it('allows action for feature header rows', () => {
    expect(isCheckoutEligibleRow(`${FEATURE_HEADER_PREFIX}feature-alpha`)).toBe(true);
  });

  it('no-ops for non-feature rows (task + status-group feature)', () => {
    expect(isCheckoutEligibleRow('task-1')).toBe(false);
    expect(isCheckoutEligibleRow(`${COMPLETED_FEATURE_PREFIX}feature-alpha`)).toBe(false);
  });
});

describe('App feature checkout success logging', () => {
  it('formats created task success message with task id/title', () => {
    const message = formatCheckoutSuccessMessage({
      created: true,
      taskId: 'abc12def',
      taskTitle: 'Run feature checkout',
    });

    expect(message).toBe('Created checkout task: abc12def - Run feature checkout');
  });

  it('treats created=false as success and formats reused message', () => {
    const message = formatCheckoutSuccessMessage({
      created: false,
      taskId: 'xyz98abc',
      taskTitle: 'Run feature checkout',
    });

    expect(message).toBe('Reused checkout task: xyz98abc - Run feature checkout');
    expect(message.includes('Failed to mark feature checkout')).toBe(false);
  });
});
