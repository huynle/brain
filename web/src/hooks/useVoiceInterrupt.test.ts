import test from 'node:test';
import assert from 'node:assert/strict';
import { VoiceInterruptDetector } from './useVoiceInterrupt';
test('interrupt detector ignores noise and brief clicks, requires sustained voice, resets between turns',()=>{
 const d=new VoiceInterruptDetector();
 assert.equal(d.update(.001,0),false);
 assert.equal(d.update(.06,20),false);
 assert.equal(d.update(.001,100),false);
 assert.equal(d.update(.06,200),false);
 assert.equal(d.update(.06,370),false);
 assert.equal(d.update(.06,390),true);
 d.reset();
 assert.equal(d.update(.06,500),false);
});
test('quieter sustained voice interrupts but low background noise does not',()=>{
 const d=new VoiceInterruptDetector();
 assert.equal(d.update(.003,0),false);
 assert.equal(d.update(.02,50),false);
 assert.equal(d.update(.02,240),true);
});
