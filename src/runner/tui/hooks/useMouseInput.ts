import { useEffect } from 'react';
import { useStdin } from 'ink';
import type { TUIMouseEvent } from '../types';

const SGR_MOUSE_PATTERN = /\x1b\[<(\d+);(\d+);(\d+)([mM])/g;

function decodeMouseButton(code: number): TUIMouseEvent['button'] | null {
  // Ignore wheel, drag, and motion bits for click foundation support.
  if ((code & 0b1100000) !== 0) {
    return null;
  }

  const baseCode = code & 0b11;
  if (baseCode === 0) return 'left';
  if (baseCode === 1) return 'middle';
  if (baseCode === 2) return 'right';
  return null;
}

export function parseMouseInput(input: string): TUIMouseEvent[] {
  if (!input) return [];

  const events: TUIMouseEvent[] = [];
  SGR_MOUSE_PATTERN.lastIndex = 0;

  for (const match of input.matchAll(SGR_MOUSE_PATTERN)) {
    const [, rawCode, rawColumn, rawRow, action] = match;
    if (action !== 'M') {
      continue;
    }

    const code = Number.parseInt(rawCode, 10);
    const column = Number.parseInt(rawColumn, 10);
    const row = Number.parseInt(rawRow, 10);

    if (!Number.isFinite(code) || !Number.isFinite(column) || !Number.isFinite(row)) {
      continue;
    }

    const button = decodeMouseButton(code);
    if (!button) {
      continue;
    }

    events.push({ button, column, row });
  }

  return events;
}

type WriteFn = (chunk: string) => unknown;

const ENABLE_MOUSE_SEQUENCES = ['\x1b[?1000h', '\x1b[?1006h'];
const DISABLE_MOUSE_SEQUENCES = ['\x1b[?1000l', '\x1b[?1006l'];

/**
 * Enable/disable terminal mouse mode (X10 + SGR), silently ignoring unsupported terminals.
 */
export function setTerminalMouseMode(enabled: boolean, write: WriteFn = process.stdout.write.bind(process.stdout)): void {
  const sequences = enabled ? ENABLE_MOUSE_SEQUENCES : DISABLE_MOUSE_SEQUENCES;

  for (const sequence of sequences) {
    try {
      write(sequence);
    } catch {
      // Silent fallback: terminal may not support mouse mode.
    }
  }
}

/**
 * Listen for raw stdin data and emit parsed TUI mouse events.
 */
export function useMouseInput(onMouseEvent: (event: TUIMouseEvent) => void): void {
  const { stdin } = useStdin();

  useEffect(() => {
    const handleInput = (data: Buffer | string) => {
      const chunk = typeof data === 'string' ? data : data.toString('utf8');
      const events = parseMouseInput(chunk);
      for (const event of events) {
        onMouseEvent(event);
      }
    };

    stdin.on('data', handleInput);
    return () => {
      stdin.off('data', handleInput);
    };
  }, [stdin, onMouseEvent]);
}
