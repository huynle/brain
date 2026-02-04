/**
 * Hook to track terminal size and re-render on resize
 */

import { useState, useEffect } from 'react';
import { useStdout } from 'ink';

export interface TerminalSize {
  columns: number;
  rows: number;
}

/**
 * Get the current terminal dimensions and update on resize.
 * Falls back to 80x24 if dimensions cannot be determined.
 */
export function useTerminalSize(): TerminalSize {
  const { stdout } = useStdout();
  
  const getSize = (): TerminalSize => ({
    columns: stdout.columns || process.stdout.columns || 80,
    rows: stdout.rows || process.stdout.rows || 24,
  });

  const [size, setSize] = useState<TerminalSize>(getSize);

  useEffect(() => {
    const handleResize = () => {
      setSize(getSize());
    };

    // Listen for resize events
    stdout.on('resize', handleResize);
    process.stdout.on('resize', handleResize);

    return () => {
      stdout.off('resize', handleResize);
      process.stdout.off('resize', handleResize);
    };
  }, [stdout]);

  return size;
}
