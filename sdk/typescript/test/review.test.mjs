import {test} from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import {inspect} from 'node:util';
import {Console} from 'node:console';
import {Writable} from 'node:stream';
import {BrainClient,BrainError} from '../dist/index.js';

async function serve(t,handler,config={}){
 const server=http.createServer(handler);await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 t.after(()=>{server.closeAllConnections();server.close()});
 return new BrainClient({baseUrl:`http://127.0.0.1:${server.address().port}`,...config});
}

test('ordinary Node inspection does not disclose response content',async t=>{
 const secret='sensitive_response_content';
 const c=await serve(t,(_req,res)=>{res.writeHead(403);res.end(JSON.stringify({message:secret,details:[{field:secret,message:secret}]}))});
 const error=await c.health().catch(e=>e);assert.ok(error instanceof BrainError);
 assert.equal(error.serverMessage,secret);assert.equal(error.details[0].message,secret);
 let logged='';const stream=new Writable({write(chunk,_encoding,done){logged+=chunk;done()}});
 new Console({stdout:stream,stderr:stream}).error(error);
 for(const text of [inspect(error),logged,JSON.stringify(error)]) assert.equal(text.includes(secret),false,text);
});

test('mid-response disconnect preserves SDK error metadata',async t=>{
 let disconnect;
 const c=await serve(t,(_req,res)=>{
  res.writeHead(200,{'X-Request-ID':'disconnect-id','Content-Length':'1000'});
  res.write('{"partial":');disconnect=()=>res.socket?.destroy();
 },{fetch:async(url,init)=>{const response=await fetch(url,init);disconnect();return response}});
 const error=await c.health().catch(e=>e);
 assert.ok(error instanceof BrainError,`expected BrainError, received ${error}`);
 assert.equal(error.code,'response_read_failed');assert.equal(error.status,200);assert.equal(error.requestId,'disconnect-id');
});

test('structural dot identifiers never reach the server',async t=>{
 let requests=0;const c=await serve(t,(_req,res)=>{requests++;res.end('{}')});
 for(const id of ['.','..','%2e%2e','%252e%252e','folder/../entry','folder\\..\\entry']) {
  for(const call of [()=>c.tasks.get(id,'health'),()=>c.entries.get(id),()=>c.goals.progress(id),()=>c.attachments.get('p',id)]) {
   await assert.rejects(async()=>await call(),e=>e instanceof BrainError&&e.code==='invalid_request',id);
  }
 }
 assert.equal(requests,0);
});

test('mid-response cancellation preserves the caller reason',async t=>{
 const controller=new AbortController();const reason=new Error('caller cancellation');
 const c=await serve(t,(_req,res)=>{res.writeHead(200,{'Content-Length':'1000'});res.write('{');},
  {fetch:async(url,init)=>{const response=await fetch(url,init);controller.abort(reason);return response}});
 await assert.rejects(c.health({signal:controller.signal}),error=>error===reason);
});
