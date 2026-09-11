import { useEffect, useRef, useState } from 'react';
import { api, assistantVoiceDiagnostic } from '../lib/api';
import type { createSpeechDetector } from '../lib/speechDetector';
import { pcmWav, VoiceSegmenter } from '../lib/voiceCapture';

const processorCode=`class Capture extends AudioWorkletProcessor {
 constructor(){super();this.buffer=new Float32Array(2048);this.offset=0;}
 process(inputs){const mono=inputs[0]?.[0];if(mono)for(const sample of mono){this.buffer[this.offset++]=sample;if(this.offset===2048){this.port.postMessage(this.buffer,[this.buffer.buffer]);this.buffer=new Float32Array(2048);this.offset=0;}}return true;}
}registerProcessor('brain-capture',Capture);`;

export function usePersistentVoice(options: {sessionId:string; active:boolean; speaking:boolean; isBusy:()=>boolean; onStartSpeech:()=>void; onTurn:(text:string)=>void; onEnabled:(value:boolean)=>void}) {
  const latest=useRef(options);latest.current=options;
  const generation=useRef(0), cleanup=useRef<(()=>void)|null>(null);
  const [enabled,setEnabled]=useState(false),[status,setStatus]=useState(''),[error,setError]=useState('');
  const stop=()=>{generation.current++;cleanup.current?.();cleanup.current=null;setEnabled(false);setStatus('');latest.current.onEnabled(false);};
  useEffect(()=>{if(!options.active)stop();return stop;},[options.active,options.sessionId]);
  const start=async()=>{
    stop();const run=++generation.current;setError('');setStatus('Connecting microphone…');setEnabled(true);latest.current.onEnabled(true);
    let stream:MediaStream|undefined, context:AudioContext|undefined, node:AudioWorkletNode|undefined, timer:ReturnType<typeof setInterval>|undefined;
    const abort=new AbortController(), attempt=crypto.randomUUID(), began=performance.now();
    let results=0,disposed=false;
    let startupError='Microphone could not start. Allow microphone access and try again.';
    let detector: Awaited<ReturnType<typeof createSpeechDetector>> | undefined;
    const current=()=>generation.current===run&&!disposed;
    const report=(event:string)=>void assistantVoiceDiagnostic({attempt,event,error:'',elapsed_ms:Math.min(86400000,Math.round(performance.now()-began)),results,android:/Android/i.test(navigator.userAgent),hands_free:true}).catch(()=>{});
    cleanup.current=()=>{disposed=true;void detector?.close().catch(()=>{});abort.abort();clearInterval(timer);if(node){node.port.onmessage=null;node.disconnect();}stream?.getTracks().forEach(t=>t.stop());if(context&&context.state!=='closed')void context.close().catch(()=>{});report('stopped');};
    const fail=(message:string)=>{if(!current())return;stop();setError(message);};
    try {
      // Own one audio context and one stream for the whole session.
      context=new AudioContext();void context.resume().catch(()=>{});
      report('start_requested');
      stream=await navigator.mediaDevices.getUserMedia({audio:{echoCancellation:true,noiseSuppression:true,autoGainControl:true,channelCount:1}});
      if(!current()){stream.getTracks().forEach(t=>t.stop());return;}
      const track=stream.getAudioTracks()[0];
      track.onended=()=>fail('Microphone disconnected. Reconnect it and start hands-free again.');
      const canInterrupt=track.getSettings().echoCancellation===true;
      const moduleURL=URL.createObjectURL(new Blob([processorCode],{type:'text/javascript'}));
      try{await context.audioWorklet.addModule(moduleURL);}finally{URL.revokeObjectURL(moduleURL);}
      if(!current())return;
      await context.resume();if(!current())return;
      setStatus('Loading speech detector…');
      startupError='Speech detector could not load. Check your connection and restart hands-free.';
      const {createSpeechDetector}=await import('../lib/speechDetector');
      if(!current())return;
      detector=await createSpeechDetector(context.sampleRate);
      if(!current()){await detector.close();return;}
      const rate=16000, queue:Float32Array[]=[];let processing=false;
      const processQueue=async()=>{
        if(processing)return;processing=true;
        try{while(queue.length&&current()){
          const bytes=pcmWav(queue.shift()!,rate);let binary='';for(let i=0;i<bytes.length;i+=8192)binary+=String.fromCharCode(...bytes.subarray(i,i+8192));
          setStatus('Transcribing…');
          const response=await api<{text:string}>('/api/v1/assistant/transcribe',{method:'POST',body:{audio:btoa(binary)},signal:abort.signal});
          if(!current())return;
          if(response.text.trim()){
            results++;report('result');
            while(current()&&latest.current.isBusy())await new Promise(r=>setTimeout(r,100));
            if(current())latest.current.onTurn(response.text.trim());
          }
          if(current())setStatus('Listening…');
        }}catch{fail('Transcription failed. Start hands-free to retry, or use Speak.');}finally{processing=false;}
      };
      const segmenter=new VoiceSegmenter(rate,()=>{if(!current())return;report('speech_start');latest.current.onStartSpeech();setStatus('Hearing you…');},pcm=>{
        if(!current())return;if(queue.length>=3){fail('Speech processing fell behind. Please restart hands-free.');return;}
        queue.push(pcm);void processQueue();
      });
      node=new AudioWorkletNode(context,'brain-capture');
      const pcmQueue:Float32Array[]=[];let detecting=false;
      const detect=async()=>{
        if(detecting)return;detecting=true;
        try{while(pcmQueue.length&&current())await detector!.process(pcmQueue.shift()!, (frame, probability)=>{
          if(!current())return;
          if(latest.current.speaking&&!canInterrupt){segmenter.reset();return;}
          segmenter.push(frame,probability,latest.current.speaking);
        });}catch{fail('Speech detection failed. Please restart hands-free.');}finally{detecting=false;}
      };
      node.port.onmessage=event=>{if(!current())return;if(pcmQueue.length>=48){fail('Speech detection cannot keep up on this device. Please restart hands-free.');return;}pcmQueue.push(event.data);void detect();};
      const source=context.createMediaStreamSource(stream),silent=context.createGain();silent.gain.value=0;
      source.connect(node);node.connect(silent);silent.connect(context.destination);
      report('audio_start');setStatus('Listening…');
      timer=setInterval(()=>{if(!current())return;report('waiting');if(track.muted||context?.state!=='running')setStatus('Microphone paused by browser. Tap End hands-free, then restart.');},30000);
    }catch{fail(startupError);}
  };
  useEffect(()=>{const hidden=()=>{if(document.hidden)stop();};document.addEventListener('visibilitychange',hidden);return()=>document.removeEventListener('visibilitychange',hidden);},[]);
  return {start,stop,enabled,status,error};
}
