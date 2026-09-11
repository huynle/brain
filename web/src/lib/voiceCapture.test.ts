import {test} from 'node:test';
import assert from 'node:assert/strict';
import {VoiceSegmenter,pcmWav} from './voiceCapture';
test('silence does not send; two phrases retain pre-roll and segment without closing capture',()=>{
 let starts=0;const segments:Float32Array[]=[];
 const s=new VoiceSegmenter(16000,()=>starts++,p=>segments.push(p));
 const silence=()=>new Float32Array(1600),speech=()=>new Float32Array(1600).fill(.05);
 for(let i=0;i<600;i++)s.push(silence());
 assert.equal(starts,0);assert.equal(segments.length,0);
 for(let turn=0;turn<2;turn++){for(let i=0;i<10;i++)s.push(speech());for(let i=0;i<15;i++)s.push(silence());}
 assert.equal(starts,2);assert.equal(segments.length,2);
 assert(segments.every(p=>p.length>=16000));
 const wav=pcmWav(segments[0],16000);const view=new DataView(wav.buffer);assert.equal(view.getUint32(24,true),16000);assert.equal(view.getUint32(40,true),segments[0].length*2);
});
test('long utterances stay bounded and reset discards unfinished speech',()=>{
 let sent=0;const s=new VoiceSegmenter(16000,()=>{},p=>{sent++;assert(p.length<=16000*30+1600);});
 for(let i=0;i<310;i++)s.push(new Float32Array(1600).fill(.05));
 assert.equal(sent,1);s.reset();for(let i=0;i<20;i++)s.push(new Float32Array(1600));assert.equal(sent,1);
});
