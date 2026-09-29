import {chromium,expect} from '@playwright/test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
const wav=fs.readFileSync(new URL('./fixtures/voice-speech.wav',import.meta.url));
let dataOffset=12;while(wav.toString('ascii',dataOffset,dataOffset+4)!=='data')dataOffset+=8+wav.readUInt32LE(dataOffset+4)+(wav.readUInt32LE(dataOffset+4)%2);
const speechPCM=[];for(let i=dataOffset+8;i<dataOffset+8+wav.readUInt32LE(dataOffset+4);i+=2)speechPCM.push(wav.readInt16LE(i)/32768);
const browser=await chromium.launch();
try{
 const context=await browser.newContext({viewport:{width:390,height:844},serviceWorkers:'block'});
 if(process.env.BRAIN_TEST_TOKEN_FILE) {
   const {token}=JSON.parse(fs.readFileSync(process.env.BRAIN_TEST_TOKEN_FILE,'utf8'));
   await context.addInitScript(token=>{localStorage.setItem('brain.access_token',token);localStorage.setItem('brain.auth_mode','manual');},token);
 }
 await context.addInitScript(()=>{
  window.wakeRequests=0;window.wakeReleases=0;
  Object.defineProperty(navigator,'wakeLock',{configurable:true,value:{request:async()=>{
    window.wakeRequests++;
    const lock=new EventTarget();lock.released=false;
    lock.release=async()=>{if(!lock.released){lock.released=true;window.wakeReleases++;lock.dispatchEvent(new Event('release'));}};
    return lock;
  }}});
  window.legacyStarts=0;window.SpeechRecognition=class{start(){window.legacyStarts++;throw new Error("legacy recognizer must not start");}};window.captures=0;window.stops=0;window.audioPauses=0;
  navigator.mediaDevices.getUserMedia=async()=>{window.captures++;const track={stop(){window.stops++;},getSettings(){return {echoCancellation:true};}};return {getAudioTracks:()=>[track],getTracks:()=>[track]};};
  window.AudioContext=class{sampleRate=16000;state='running';destination={};audioWorklet={addModule:async()=>{}};resume=async()=>{};close=async()=>{this.state='closed';};createGain(){return {gain:{value:0},connect(){}};}createMediaStreamSource(){return {connect(){}};}};
  window.AudioWorkletNode=class{constructor(){this.port={onmessage:null};window.capture=this;}connect(){}disconnect(){}};
  window.Audio=class{constructor(){window.audio=this;}play=async()=>{};pause(){window.audioPauses++;}};
  window.emit=async(voice=true)=>{
    const samples=voice?window.speechPCM:Array.from({length:16000*5},(_,i)=>.1*Math.sin(2*Math.PI*70*i/16000)+.02*Math.sin(2*Math.PI*140*i/16000)+(i%12000<120?.6:0));
    for(let i=0;i<samples.length;i+=2048){const frame=new Float32Array(2048);frame.set(samples.slice(i,i+2048));window.capture.port.onmessage?.({data:frame});await new Promise(r=>setTimeout(r,10));}
    for(let i=0;i<15;i++){window.capture.port.onmessage?.({data:new Float32Array(2048)});await new Promise(r=>setTimeout(r,10));}
  };
 });
 await context.addInitScript(pcm=>{window.speechPCM=pcm;},speechPCM.slice(0,16000*3));
 const page=await context.newPage();let ready=false,ack=0,inbox;const spoken=[];
 await page.route('**/api/v1/assistant/jobs?**',route=>route.fulfill({json:{jobs:ready?[{id:'job',title:'Count entries',state:'completed',revision:2,acknowledged:ack}]:[],conversations:[]}}));
 await page.route('**/api/v1/assistant/chat/stream',route=>{
   if(route.request().postDataJSON().inbox){inbox=route;return;}
   return route.fulfill({contentType:'application/x-ndjson',body:'{"type":"done","reply":"I heard your follow-up."}\n'});
 });
 await page.route('**/api/v1/assistant/transcribe',route=>route.fulfill({json:{text:'Here is my follow-up'}}));
 await page.route('**/api/v1/assistant/speech',route=>{spoken.push(route.request().postDataJSON().text);return route.fulfill({contentType:'audio/mpeg',body:'ID3test'});});
 await page.goto(process.env.BRAIN_PREVIEW_URL||'http://localhost:3343');
 await page.getByRole('button',{name:'Assistant',exact:true}).click();
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 await expect(page.getByText('Listening…',{exact:true})).toBeVisible({timeout:30000});
 ready=true;
 await expect.poll(()=>Boolean(inbox),{timeout:10000}).toBe(true);
 await page.evaluate(async()=>{const samples=window.speechPCM;for(let i=0;i<16000*2;i+=2048){const frame=new Float32Array(2048);frame.set(samples.slice(i,i+2048));window.capture.port.onmessage?.({data:frame});await new Promise(r=>setTimeout(r,25));}});
 await expect(page.getByText('Hearing you…',{exact:true})).toBeVisible();
 ack=2;await inbox.fulfill({contentType:'application/x-ndjson',body:'{"type":"done","reply":"Your entry count is ready."}\n'});
 await expect(page.getByText('Your entry count is ready.',{exact:true})).toBeVisible();
 assert.equal(spoken.length,0,'job summary must not interrupt the user');
 await page.evaluate(async()=>{for(let i=0;i<20;i++){window.capture.port.onmessage?.({data:new Float32Array(2048)});await new Promise(r=>setTimeout(r,25));}});
 await expect.poll(()=>spoken[0],{timeout:15000}).toBe('I heard your follow-up.');
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toBeVisible();
 await page.evaluate(()=>window.audio?.onended?.());
 await expect.poll(()=>spoken[1],{timeout:15000}).toBe('Your entry count is ready.');
 console.log('PASS inbox result waits through speech and the foreground reply, then plays once');
}finally{await browser.close();}
