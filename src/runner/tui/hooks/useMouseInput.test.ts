import { describe, expect, it } from 'bun:test';
import { parseMouseInput, setTerminalMouseMode } from './useMouseInput';

describe('parseMouseInput', () => {
  it('parses left click SGR mouse sequences', () => {
    expect(parseMouseInput('\u001b[<0;12;7M')).toEqual([
      { button: 'left', column: 12, row: 7 },
    ]);
  });

  it('parses right click SGR mouse sequences', () => {
    expect(parseMouseInput('\u001b[<2;40;9M')).toEqual([
      { button: 'right', column: 40, row: 9 },
    ]);
  });

  it('parses middle click SGR mouse sequences', () => {
    expect(parseMouseInput('\u001b[<1;21;6M')).toEqual([
      { button: 'middle', column: 21, row: 6 },
    ]);
  });

  it('ignores release and unsupported buttons', () => {
    expect(parseMouseInput('\u001b[<0;12;7m')).toEqual([]);
    expect(parseMouseInput('\u001b[<64;12;7M')).toEqual([]);
  });

  it('ignores drag/motion mouse sequences for click-only handling', () => {
    expect(parseMouseInput('\u001b[<32;12;7M')).toEqual([]);
    expect(parseMouseInput('\u001b[<35;12;7M')).toEqual([]);
  });

  it('extracts multiple events from mixed terminal output', () => {
    expect(parseMouseInput('abc\u001b[<0;1;2Mdef\u001b[<2;3;4M')).toEqual([
      { button: 'left', column: 1, row: 2 },
      { button: 'right', column: 3, row: 4 },
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

    expect(writes).toEqual(['\u001b[?1000h', '\u001b[?1006h']);
  });

  it('writes disable sequences for mouse reporting', () => {
    const writes: string[] = [];
    setTerminalMouseMode(false, (chunk) => {
      writes.push(chunk);
    });

    expect(writes).toEqual(['\u001b[?1000l', '\u001b[?1006l']);
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

    expect(callCount).toBe(2);
  });
});
