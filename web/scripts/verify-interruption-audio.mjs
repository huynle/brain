import { chromium, expect } from '@playwright/test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'brain-mic-'));
const input=path.join(dir,'input.wav');
const count=288000, rate=48000;
const wav=Buffer.alloc(44+count*2);
wav.write('RIFF');wav.writeUInt32LE(36+count*2,4);wav.write('WAVEfmt ',8);
wav.writeUInt32LE(16,16);wav.writeUInt16LE(1,20);wav.writeUInt16LE(1,22);
wav.writeUInt32LE(rate,24);wav.writeUInt32LE(rate*2,28);wav.writeUInt16LE(2,32);wav.writeUInt16LE(16,34);
wav.write('data',36);wav.writeUInt32LE(count*2,40);
for(let i=0;i<count;i++) wav.writeInt16LE(Math.round(32767*(i<96000?.002:.07)*Math.sin(2*Math.PI*220*i/rate)),44+i*2);
fs.writeFileSync(input,wav);
const browser=await chromium.launch({args:['--use-fake-ui-for-media-stream','--use-fake-device-for-media-stream',`--use-file-for-fake-audio-capture=${input}`]});
try {
 const context=await browser.newContext({viewport:{width:390,height:844},serviceWorkers:'block'});
 await context.addInitScript(()=>{
  window.started=0;window.aborted=0;window.micLevel=0;
  window.SpeechRecognition=class {
   start(){window.rec=this;window.started++;this.onaudiostart?.();}
   stop(){this.onend?.();}
   abort(){window.aborted++;this.onend?.();}
  };
  window.Audio=class {constructor(){window.audio=this;} async play(){} pause(){} };
 });
 const page=await context.newPage();let turns=[];
 await page.route('**/api/v1/assistant/chat/stream',route=>{
  turns.push(route.request().postDataJSON());
  return route.fulfill({contentType:'application/x-ndjson',body:'{"type":"delta","delta":"Hello back."}\n{"type":"done","reply":"Hello back."}\n'});
 });
 await page.route('**/api/v1/assistant/speech',route=>route.fulfill({contentType:'audio/mpeg',body:'ID3fixture'}));
 await page.goto('http://localhost:3340');
 await page.getByRole('button',{name:'Assistant',exact:true}).click({timeout:30000});
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 await expect(page.getByText('Listening… Your message sends after a pause.',{exact:true})).toBeVisible();
 await page.evaluate(()=>window.rec.onresult({results:[[{transcript:'First voice turn'}]]}));
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toBeVisible({timeout:10000});
 await expect.poll(()=>page.evaluate(()=>window.started),{timeout:15000}).toBe(2);
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toHaveCount(0);
 console.log('PASS real Chromium getUserMedia + AudioContext pipeline interrupts playback from WAV microphone input');
}finally{await browser.close();fs.rmSync(dir,{recursive:true,force:true});}
