import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { once } from "node:events";
import { BrainClient, BrainError } from "../dist/index.js";

async function server(t, handler) {
  const s = createServer(handler);
  s.listen(0, "127.0.0.1"); await once(s, "listening");
  t.after(() => { s.closeAllConnections(); s.close(); });
  return `http://127.0.0.1:${s.address().port}`;
}

test("typed operations preserve binding, escaping, CAS, options and routes", async t => {
  const seen = [];
  const baseUrl = await server(t, async (req,res) => {
    assert.equal(req.headers.authorization,"Bearer secret");
    assert.equal(req.headers["x-brain-tenant"],"org-a");
    let body=""; for await (const chunk of req) body+=chunk;
    seen.push([req.method,req.url,body,req.headers]);
    if(req.method==="DELETE") {res.writeHead(204).end();return;}
    res.setHeader("Content-Type","application/json");res.end(JSON.stringify({id:"a",title:"entry",revision:"r1"}));
  });
  const config={baseUrl,token:"secret",tenant:"org-a",authGeneration:"g1"};
  const c=new BrainClient(config); t.after(()=>c.close());config.token="changed";
  assert.equal((await c.entries.get("projects/x/note/a.md")).id,"a");
  await c.entries.list({project:"p",limit:10});
  await c.entries.create({type:"note",title:"entry",content:"body"},{idempotencyKey:"k",requestId:"req"});
  await c.entries.update("a",{title:"updated",expected_revision:"r1"});
  await c.entries.delete("a");
  await c.tasks.list("p"); await c.tasks.get("p","t"); await c.search({query:"entry"}); await c.health();
  assert.deepEqual(seen.map(x=>x.slice(0,2)),[
    ["GET","/api/v1/entries/projects%2Fx%2Fnote%2Fa.md"],["GET","/api/v1/entries?project=p&limit=10"],
    ["POST","/api/v1/entries"],["PATCH","/api/v1/entries/a"],["DELETE","/api/v1/entries/a?confirm=true"],
    ["GET","/api/v1/tasks/p"],["GET","/api/v1/tasks/p/t"],["POST","/api/v1/search"],["GET","/api/v1/health"]]);
  assert.equal(seen[2][3]["idempotency-key"],"k");assert.equal(seen[2][3]["x-request-id"],"req");
  assert.equal(JSON.parse(seen[3][2]).expected_revision,"r1");
});

test("single binding omits selector and refuses redirects",async t=>{
  let leaked=false;
  const other=await server(t,(_req,res)=>{leaked=true;res.end("{}");});
  const baseUrl=await server(t,(req,res)=>{assert.equal(req.headers["x-brain-tenant"],undefined);res.writeHead(302,{Location:other}).end();});
  const c=new BrainClient({baseUrl,token:"secret"});t.after(()=>c.close());
  await assert.rejects(c.health(),e=>e instanceof BrainError&&e.code==="redirect_refused");assert.equal(leaked,false);
});

test("bounded streamed response and legacy errors",async t=>{
  for(const [status,body,code,limit] of [[200,"x".repeat(100),"response_too_large",32],[403,'{"message":"denied"}',"forbidden",100],[200,"{","invalid_response",100]]) {
    const baseUrl=await server(t,(_req,res)=>{res.writeHead(status,{"X-Request-ID":"server-req"});res.end(body);});
    const c=new BrainClient({baseUrl,maxResponseBytes:limit});t.after(()=>c.close());
    await assert.rejects(c.health(),e=>e instanceof BrainError&&e.code===code&&e.requestId==="server-req");
  }
});

test("rebind cancels requests and permanently retires old client",async t=>{
  let entered;const requestSeen=new Promise(resolve=>entered=resolve);
  const baseUrl=await server(t,()=>entered());
  const c=new BrainClient({baseUrl});t.after(()=>c.close());
  const pending=c.health();const rejection=assert.rejects(pending,e=>e.name==="AbortError");await requestSeen;
  const next=c.rebind({baseUrl,tenant:"new"});t.after(()=>next.close());
  await rejection;await assert.rejects(c.health(),e=>e.name==="AbortError");
});

test("caller cancellation and timeout abort native fetch",async t=>{
  const baseUrl=await server(t,()=>{});
  const c=new BrainClient({baseUrl,timeoutMs:20});t.after(()=>c.close());
  await assert.rejects(c.health(),e=>e.name==="TimeoutError");
  const a=new AbortController();a.abort();await assert.rejects(c.health({signal:a.signal}),e=>e.name==="AbortError");
});

test("mutations are not retried, even with idempotency key",async t=>{
  let writes=0;
  const baseUrl=await server(t,(_req,res)=>{writes++;res.writeHead(503).end('{"message":"busy"}');});
  const c=new BrainClient({baseUrl});t.after(()=>c.close());
  await assert.rejects(c.entries.create({type:"note",title:"x",content:""},{idempotencyKey:"k"}),e=>e.code==="unavailable");assert.equal(writes,1);
});

test("configuration rejects credential-bearing URLs and invalid limits",()=>{
  for(const config of [{baseUrl:"https://token@example.org"},{baseUrl:"https://example.org/?token=x"},{baseUrl:"file:///x"},{baseUrl:"https://example.org",maxResponseBytes:Infinity},{baseUrl:"https://example.org",token:"x\r\nY:z"}]) {
    assert.throws(()=>new BrainClient(config),e=>e instanceof BrainError&&e.code==="invalid_configuration");
  }
});
