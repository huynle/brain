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
   window.captures=0;const get=navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
   navigator.mediaDevices.getUserMedia=(options)=>{window.captures++;return get(options);};
   window.Audio=class{play=async()=>{};pause(){};};
 });
 const page=await context.newPage();let transcriptions=0,turns=0;
 await page.route('**/api/v1/assistant/transcribe',route=>{transcriptions++;return route.fulfill({json:{text:'Captured phrase '+transcriptions}});});
 await page.route('**/api/v1/assistant/chat/stream',route=>{turns++;return route.fulfill({contentType:'application/x-ndjson',body:'{"type":"done","reply":"Short reply."}\n'});});
 await page.route('**/api/v1/assistant/speech',route=>route.fulfill({contentType:'audio/mpeg',body:'ID3test'}));
 await page.goto(process.env.BRAIN_PREVIEW_URL||'http://localhost:3340');
 await page.getByRole('button',{name:'Assistant',exact:true}).click();
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 await expect.poll(()=>turns,{timeout:40000}).toBeGreaterThanOrEqual(2);
 assert.equal(await page.evaluate(()=>window.captures),1);
 await page.getByRole('button',{name:'End hands-free',exact:true}).click();
 console.log('PASS real AudioWorklet and microphone stream deliver multiple segmented turns without reopening capture');
}finally{await browser.close();fs.rmSync(dir,{recursive:true,force:true});}
