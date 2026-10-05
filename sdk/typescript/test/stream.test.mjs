import test from 'node:test';
import assert from 'node:assert/strict';
import {BrainClient,BrainError} from '../dist/index.js';
const event={id:'e1',type:'entry.created',source:'api',timestamp:'2026-10-05T12:00:00Z'};

test('stream caller abort, close, rebind and timeout cancel pending reads',async()=>{
 for(const mode of ['caller','close','rebind','timeout']){
  let ready;const started=new Promise(resolve=>{ready=resolve;});let cancelled=false;
  const controller=new AbortController();
  const c=new BrainClient({baseUrl:'https://example.test',timeoutMs:mode==='timeout'?30:1000,fetch:async()=>new Response(new ReadableStream({pull(){ready();},cancel(){cancelled=true;}}),{headers:{'Content-Type':'text/event-stream'}})});
  // Keep the test alive for the unref'ed AbortSignal timeout, with a hard cap.
  const watchdog=setTimeout(()=>controller.abort(new Error('watchdog')),1000);
  try{
   const done=c.events.stream({},()=>assert.fail('late event'),{signal:controller.signal});
   const outcome=assert.rejects(done,e=>e.name==='AbortError'||e.name==='TimeoutError');
   await started;
   if(mode==='caller')controller.abort();else if(mode==='close')c.close();else if(mode==='rebind')c.rebind({baseUrl:'https://example.test'}).close();
   await outcome;assert.equal(cancelled,true);
  }finally{clearTimeout(watchdog);c.close();}
 }
});

test('stream splits chunks, ignores comments, decodes multiline data, closes on consumer stop',async()=>{
 let cancelled=false,calls=0;const stop=new Error('stop');const text=': heartbeat\r\n\r\ndata: {\r\ndata: '+JSON.stringify(event).slice(1)+'\r\n\r\ndata: '+JSON.stringify(event)+'\n\n';
 const c=new BrainClient({baseUrl:'https://example.test',token:'secret',fetch:async(url,init)=>{
  assert.equal(url,'https://example.test/api/v1/events/stream?project_id=p+q');assert.equal(init.headers.get('Last-Event-ID'),'old');assert.equal(init.headers.get('Authorization'),'Bearer secret');assert.equal(init.headers.get('Accept'),'text/event-stream');
  let at=0;return new Response(new ReadableStream({pull(controller){if(at<text.length)controller.enqueue(new TextEncoder().encode(text.slice(at,++at)));},cancel(){cancelled=true;}}),{headers:{'Content-Type':'text/event-stream'}});
 }});
 assert.equal(typeof c.events.stream,'function');
 await assert.rejects(c.events.stream({project_id:'p q'},e=>{calls++;assert.deepEqual(e,event);throw stop;},{lastEventId:'old'}),e=>e===stop);
 assert.equal(calls,1);assert.equal(cancelled,true);c.close();
});

test('stream refuses oversized frames and malformed or non-event JSON',async()=>{
 for(const [body,code]of [['data: '+ 'x'.repeat(2048)+'\n\n','response_too_large'],[': small\n'.repeat(300)+'\n','response_too_large'],['data: null\n\n','invalid_response'],['data: nope\n\n','invalid_response']]){
 const c=new BrainClient({baseUrl:'https://example.test',maxResponseBytes:1024,fetch:async()=>new Response(body,{headers:{'Content-Type':'text/event-stream','X-Request-ID':'stream-request'}})});
 assert.equal(typeof c.events.stream,'function');
 await assert.rejects(c.events.stream({},()=>assert.fail('unexpected callback')),e=>e instanceof BrainError&&e.code===code&&e.requestId==='stream-request');c.close();
 }
});

test('stream ignores incomplete EOF and preserves legacy HTTP failures',async()=>{
 for(const [status,body,ctype,code]of [[200,'data: '+JSON.stringify(event),'text/event-stream',null],[200,'{}','application/json','invalid_response'],[401,'{"message":"denied"}','application/json','unauthorized'],[302,'','text/event-stream','redirect_refused']]){
 const c=new BrainClient({baseUrl:'https://example.test',fetch:async()=>new Response(body,{status,headers:{'Content-Type':ctype}})});assert.equal(typeof c.events.stream,'function');const p=c.events.stream({},()=>assert.fail('unexpected callback'));if(code)await assert.rejects(p,e=>e instanceof BrainError&&e.code===code);else await p;c.close();}
});
