import { describe, expect, it } from 'bun:test';
import { parseMouseInput, setTerminalMouseMode } from './useMouseInput';

describe('parseMouseInput', () => {
  it('parses left click SGR mouse sequences', () => {
    expect(parseMouseInput('\u001b[<0;12;7M')).toEqual([
      { kind: 'press', button: 'left', column: 12, row: 7 },
    ]);
  });

  it('parses right click SGR mouse sequences', () => {
    expect(parseMouseInput('\u001b[<2;40;9M')).toEqual([
      { kind: 'press', button: 'right', column: 40, row: 9 },
    ]);
  });

  it('parses middle click SGR mouse sequences', () => {
    expect(parseMouseInput('\u001b[<1;21;6M')).toEqual([
      { kind: 'press', button: 'middle', column: 21, row: 6 },
    ]);
  });

  it('parses mouse motion SGR sequences', () => {
    expect(parseMouseInput('\u001b[<35;12;7M')).toEqual([
      { kind: 'move', button: 'none', column: 12, row: 7 },
    ]);
  });

  it('ignores release and unsupported buttons', () => {
    expect(parseMouseInput('\u001b[<0;12;7m')).toEqual([]);
    expect(parseMouseInput('\u001b[<64;12;7M')).toEqual([]);
  });

  it('parses drag as move events', () => {
    expect(parseMouseInput('\u001b[<32;12;7M')).toEqual([
      { kind: 'move', button: 'left', column: 12, row: 7 },
    ]);
    expect(parseMouseInput('\u001b[<34;12;7M')).toEqual([
      { kind: 'move', button: 'right', column: 12, row: 7 },
    ]);
  });

  it('extracts multiple events from mixed terminal output', () => {
    expect(parseMouseInput('abc\u001b[<0;1;2Mdef\u001b[<2;3;4M')).toEqual([
      { kind: 'press', button: 'left', column: 1, row: 2 },
      { kind: 'press', button: 'right', column: 3, row: 4 },
    ]);
  });

  it('keeps hover-motion events before click events in mixed streams', () => {
    expect(parseMouseInput('\u001b[<35;9;7M\u001b[<0;9;7M')).toEqual([
      { kind: 'move', button: 'none', column: 9, row: 7 },
      { kind: 'press', button: 'left', column: 9, row: 7 },
    ]);
  });

  it('silently ignores malformed and incomplete input', () => {
    expect(() => parseMouseInput('')).not.toThrow();
    expect(() => parseMouseInput('plain-text')).not.toThrow();
    expect(() => parseMouseInput('\u001b[<x;y;zM')).not.toThrow();
    expect(() => parseMouseInput('\u001b[<0;12')).not.toThrow();

    expect(parseMouseInput('\u001b[<x;y;zM')).toEqual([]);
    expect(parseMouseInput('\u001b[<0;12')).toEqual([]);
  });
});

describe('setTerminalMouseMode', () => {
  it('writes enable sequences for mouse reporting', () => {
    const writes: string[] = [];
    setTerminalMouseMode(true, (chunk) => {
      writes.push(chunk);
    });

    expect(writes).toEqual(['\u001b[?1000h', '\u001b[?1003h', '\u001b[?1006h']);
  });

  it('writes disable sequences for mouse reporting', () => {
    const writes: string[] = [];
    setTerminalMouseMode(false, (chunk) => {
      writes.push(chunk);
    });

    expect(writes).toEqual(['\u001b[?1000l', '\u001b[?1003l', '\u001b[?1006l']);
  });

  it('silently swallows write errors', () => {
    expect(() => {
      setTerminalMouseMode(true, () => {
        throw new Error('write-failed');
      });
    }).not.toThrow();
  });

  it('continues writing remaining sequences after a write failure', () => {
    let callCount = 0;

    expect(() => {
      setTerminalMouseMode(true, () => {
        callCount += 1;
        if (callCount === 1) {
          throw new Error('first-write-failed');
        }
      });
    }).not.toThrow();

    expect(callCount).toBe(3);
  });
});
