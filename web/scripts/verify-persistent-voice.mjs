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
 const page=await context.newPage();let modelRequests=0;page.on('request',request=>{if(request.url().includes('/assets/voice/'))modelRequests++;});let transcriptions=0;const turns=[];let held;
 await page.route('**/api/v1/assistant/transcribe',route=>{transcriptions++;const body=route.request().postDataJSON();assert(Buffer.from(body.audio,'base64').subarray(0,4).toString()==='RIFF');if(transcriptions===3){held=route;return;}return route.fulfill({json:{text:'Voice turn '+transcriptions}});});
 await page.route('**/api/v1/assistant/chat/stream',route=>{turns.push(route.request().postDataJSON());return route.fulfill({contentType:'application/x-ndjson',body:'{"type":"done","reply":"A short reply."}\n'});});
 await page.route('**/api/v1/assistant/speech',route=>route.fulfill({contentType:'audio/mpeg',body:'ID3test'}));
 await page.goto(process.env.BRAIN_PREVIEW_URL||'http://localhost:3340');
 await page.getByRole('button',{name:'Assistant',exact:true}).click();
 assert.equal(modelRequests,0,'dashboard must not eagerly download the speech model');
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 await expect(page.getByText('Listening…',{exact:true})).toBeVisible({timeout:30000});
 await page.evaluate(()=>window.emit(false));assert.equal(transcriptions,0,'road noise and brief bumps must remain local');
 await page.evaluate(()=>window.emit());
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toBeVisible();
 const pauses=await page.evaluate(()=>window.audioPauses);
 await page.evaluate(()=>window.emit(false));
 assert.equal(await page.evaluate(()=>window.audioPauses),pauses,'road rumble must not interrupt a reply');
 assert.equal(transcriptions,1);
 await page.evaluate(()=>window.emit());
 await expect.poll(()=>turns.length,{timeout:30000}).toBe(2);
 assert.equal(turns[1].message,'Voice turn 2');assert(turns[1].history.length>0);
 assert.equal(await page.evaluate(()=>window.captures),1,'one capture across turns');
 assert.equal(await page.evaluate(()=>window.legacyStarts),0,'browser recognition must not compete with persistent capture');
 assert.equal(await page.evaluate(()=>window.stops),0,'microphone stays open');
 assert((await page.evaluate(()=>window.audioPauses))>0,'barge-in stops audio');
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toBeVisible();
 await page.evaluate(()=>window.emit());await expect.poll(()=>Boolean(held)).toBe(true);
 await page.getByRole('button',{name:'New chat',exact:true}).click();
 await held.fulfill({json:{text:'stale transcription'}}).catch(()=>{});
 assert.equal(await page.evaluate(()=>window.stops),1);
 await expect(page.getByRole('button',{name:'Start hands-free',exact:true})).toBeVisible();
 await expect(page.locator('.assistant-msg')).toHaveCount(0);
 assert.equal(turns.length,2,'stale transcription cannot send into new session');
 await page.route('**/api/v1/assistant/transcribe',route=>route.fulfill({status:502,json:{error:'provider failed'}}));
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 await expect(page.getByText('Listening…',{exact:true})).toBeVisible();
 await page.evaluate(()=>window.emit());
 await expect(page.getByText('Transcription failed. Start hands-free to retry, or use Speak.',{exact:true})).toBeVisible();
 assert.equal(await page.evaluate(()=>window.stops),2,'provider failure releases the microphone');
 assert.equal(turns.length,2);
 await page.route('**/silero_vad_v5.onnx',route=>route.fulfill({status:503,body:'unavailable'}));
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 await expect(page.getByText('Speech detector could not load. Check your connection and restart hands-free.',{exact:true})).toBeVisible();
 assert.equal(await page.evaluate(()=>window.stops),3,'model load failure releases the microphone');
 await page.screenshot({path:'/tmp/brain-persistent-voice.png'});
 console.log('PASS persistent capture: local silence, two turns, barge-in, one stream, session switch releases mic and cancels transcription');
}finally{await browser.close();}
