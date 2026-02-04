/**
 * useTerminalSize hook tests
 */

import { describe, it, expect } from 'bun:test';
import React from 'react';
import { render } from 'ink-testing-library';
import { Text, Box } from 'ink';
import { useTerminalSize } from './useTerminalSize';

// Test component that displays terminal size
function TestComponent() {
  const { columns, rows } = useTerminalSize();
  return (
    <Box>
      <Text>cols={columns} rows={rows}</Text>
    </Box>
  );
}

describe('useTerminalSize', () => {
  it('should return valid terminal dimensions', () => {
    const { lastFrame, unmount } = render(<TestComponent />);
    const output = lastFrame() || '';
    
    // Should contain both cols and rows with numeric values
    expect(output).toContain('cols=');
    expect(output).toContain('rows=');
    
    // Extract values and verify they are positive numbers
    const colsMatch = output.match(/cols=(\d+)/);
    const rowsMatch = output.match(/rows=(\d+)/);
    
    expect(colsMatch).toBeTruthy();
    expect(rowsMatch).toBeTruthy();
    
    const cols = parseInt(colsMatch![1], 10);
    const rows = parseInt(rowsMatch![1], 10);
    
    // Should be positive and reasonable
    expect(cols).toBeGreaterThan(0);
    expect(rows).toBeGreaterThan(0);
    expect(cols).toBeLessThan(1000); // Reasonable upper bound
    expect(rows).toBeLessThan(500);
    
    unmount();
  });

  it('should provide dimensions that allow layout', () => {
    const { lastFrame, unmount } = render(<TestComponent />);
    const output = lastFrame() || '';
    
    const colsMatch = output.match(/cols=(\d+)/);
    const rowsMatch = output.match(/rows=(\d+)/);
    
    const cols = parseInt(colsMatch![1], 10);
    const rows = parseInt(rowsMatch![1], 10);
    
    // Verify minimum usable dimensions
    expect(cols).toBeGreaterThanOrEqual(20);
    expect(rows).toBeGreaterThanOrEqual(5);
    
    unmount();
  });
});
