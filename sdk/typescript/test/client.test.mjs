import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { once } from "node:events";
import { BrainClient, BrainError } from "../dist/index.js";

test("attachment binary bodies and bounds",async t=>{
 let posts=0;
 const baseUrl=await server(t,async(req,res)=>{
  if(req.method==="POST"){
   posts++; const chunks=[];for await(const chunk of req)chunks.push(chunk);
   const request=new Request("http://localhost",{method:"POST",headers:req.headers,body:Buffer.concat(chunks)});
   const form=await request.formData();assert.equal(form.get("project_id"),"p");assert.equal(form.get("metadata"),'{"source":"sdk"}');
   const file=form.get("file");assert.equal(file.name,"bytes.bin");assert.deepEqual(new Uint8Array(await file.arrayBuffer()),new Uint8Array([0,255,1,2]));
   res.writeHead(201).end('{"attachment":{"id":"a"}}');return;
  }
  res.end(Buffer.from([0,255,1,2]));
 });
 const c=new BrainClient({baseUrl});t.after(()=>c.close());assert.equal(typeof c.attachments,"object");
 const result=await c.attachments.upload("p",{filename:"bytes.bin",content:new Uint8Array([0,255,1,2]),metadata:{source:"sdk"}});assert.equal(result.attachment.id,"a");
 assert.deepEqual(await c.attachments.download("p","a"),new Uint8Array([0,255,1,2]));
 await assert.rejects(c.attachments.upload("p",{filename:"../secret",content:new Uint8Array()}),e=>e.code==="invalid_request");
 const bounded=new BrainClient({baseUrl,maxResponseBytes:3});t.after(()=>bounded.close());
 await assert.rejects(bounded.attachments.upload("p",{filename:"x",content:new Uint8Array(4)}),e=>e.code==="request_too_large");
 await assert.rejects(bounded.attachments.download("p","a"),e=>e.code==="response_too_large");assert.equal(posts,1);
});

test("attachment extraction and stored text routes",async t=>{
 const seen=[];const baseUrl=await server(t,async(req,res)=>{for await(const _ of req){};seen.push([req.method,req.url]);res.end(req.method==="GET"?"derived text":"{}");});
 const c=new BrainClient({baseUrl});t.after(()=>c.close());
 await c.attachments.extract("p","a",{attachment_id:"a",content_type:"text/plain"});
 assert.equal(await c.attachments.text("p","a"),"derived text");
 assert.deepEqual(seen,[["POST","/api/v1/attachments/a/extract?project_id=p"],["GET","/api/v1/attachments/a/text?project_id=p"]]);
});

test("legacy validation details and body request ID are preserved",async t=>{
 const baseUrl=await server(t,(_req,res)=>res.writeHead(400).end('{"error":"Validation Error","message":"Invalid request","request_id":"body-id","details":[{"field":"title","message":"required"}]}'));
 const c=new BrainClient({baseUrl});t.after(()=>c.close());
 await assert.rejects(c.health(),e=>{assert.equal(e.code,"invalid_request");assert.equal(e.requestId,"body-id");assert.deepEqual(e.details,[{field:"title",message:"required"}]);return true;});
});

test("unsafe machine codes cannot disclose content in default error formatting",async t=>{
 const baseUrl=await server(t,(_req,res)=>res.writeHead(403).end('{"code":"secret credential value","message":"sensitive content"}'));
 const c=new BrainClient({baseUrl});t.after(()=>c.close());
 await assert.rejects(c.health(),e=>e.code==="forbidden"&&!e.message.includes("secret"));
});

test("goal namespace routes",async t=>{
 const seen=[];const baseUrl=await server(t,async(req,res)=>{for await(const _ of req){};seen.push(req.method+" "+req.url);res.end("{}");});
 const c=new BrainClient({baseUrl});t.after(()=>c.close());
 await c.goals.list({project:"p",status:"all"});await c.goals.create({});await c.goals.update("g",{});await c.goals.progress("g");await c.goals.audit("g");await c.goals.run("g");await c.goals.delete("g");
 assert.deepEqual(seen,["GET /api/v1/goals?project=p&status=all","POST /api/v1/goals","PATCH /api/v1/goals/g","GET /api/v1/goals/g/progress","GET /api/v1/goals/g/audit?limit=50","POST /api/v1/goals/g/run","DELETE /api/v1/goals/g"]);
});

test("webhook namespace routes",async t=>{
 const seen=[];const baseUrl=await server(t,async(req,res)=>{for await(const _ of req){};seen.push(req.method+" "+req.url);res.end("{}");});
 const c=new BrainClient({baseUrl});t.after(()=>c.close());
 await c.webhooks.list(true);await c.webhooks.create({});await c.webhooks.get("w");await c.webhooks.update("w",{});await c.webhooks.deliveries("w");await c.webhooks.test("w");await c.webhooks.delete("w");
 assert.deepEqual(seen,["GET /api/v1/webhooks?enabled=true","POST /api/v1/webhooks","GET /api/v1/webhooks/w","PATCH /api/v1/webhooks/w","GET /api/v1/webhooks/w/deliveries?limit=50","POST /api/v1/webhooks/w/test","DELETE /api/v1/webhooks/w"]);
});

test("automation namespace routes",async t=>{
 const seen=[];const baseUrl=await server(t,async(req,res)=>{for await(const _ of req){};seen.push(req.method+" "+req.url);res.end("{}");});
 const c=new BrainClient({baseUrl});t.after(()=>c.close());
 await c.automations.run({path:"automation"});await c.automations.runs({automation_id:"a",limit:10,project:"p"});await c.automations.getRun("r");
 assert.deepEqual(seen,["POST /api/v1/automations/run","GET /api/v1/automation-runs?automation_id=a&limit=10&project=p","GET /api/v1/automation-runs/r"]);
});

test("reminder and attention namespace routes",async t=>{
 const seen=[];const baseUrl=await server(t,async(req,res)=>{for await(const _ of req){};seen.push(req.method+" "+req.url);res.end("{}");});
 const c=new BrainClient({baseUrl});t.after(()=>c.close());
 await c.reminders.list({project:"p",state:"active"});await c.reminders.get("r");await c.reminders.create({});await c.reminders.update("r",{});await c.reminders.ack("r");await c.reminders.snooze("r",{remind_at:"2030-01-01T00:00:00Z"});await c.reminders.fire("r");await c.reminders.delete("r");
 await c.attention.list({include_snoozed:true,project:"p"});await c.attention.counts();await c.attention.get("a");await c.attention.create({});await c.attention.read("a");await c.attention.unread("a");await c.attention.snooze("a",{snoozed_until:"2030-01-01T00:00:00Z"});await c.attention.resolve("a");await c.attention.dismiss("a");
 assert.deepEqual(seen,["GET /api/v1/reminders?project=p&state=active","GET /api/v1/reminders/r","POST /api/v1/reminders","PATCH /api/v1/reminders/r","POST /api/v1/reminders/r/ack","POST /api/v1/reminders/r/snooze","POST /api/v1/reminders/r/fire","DELETE /api/v1/reminders/r","GET /api/v1/attention?include_snoozed=true&project=p","GET /api/v1/attention/counts","GET /api/v1/attention/a","POST /api/v1/attention","POST /api/v1/attention/a/read","POST /api/v1/attention/a/unread","POST /api/v1/attention/a/snooze","POST /api/v1/attention/a/resolve","POST /api/v1/attention/a/dismiss"]);
});

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

test("content namespace routes and graph array decoding",async t=>{
  const seen=[];
  const baseUrl=await server(t,async(req,res)=>{
    for await(const _chunk of req) { /* drain */ }
    seen.push([req.method,req.url]);res.end(/backlinks|outlinks|related/.test(req.url)?'[{"id":"linked"}]':'{}');
  });
  const c=new BrainClient({baseUrl});t.after(()=>c.close());
  await c.entries.move("a",{project:"next"});await c.entries.bulkUpdate({});await c.entries.bulkDelete({});
  await c.sections.list("p/a.md");await c.sections.get("p/a.md","Hello world",true);
  assert.equal((await c.graph.backlinks("a"))[0].id,"linked");await c.graph.outlinks("a");await c.graph.related("a",5);
  assert.deepEqual(seen,[["POST","/api/v1/entries/a/move"],["POST","/api/v1/entries/bulk-update"],["POST","/api/v1/entries/bulk-delete"],["GET","/api/v1/entries/p%2Fa.md/sections"],["GET","/api/v1/entries/p%2Fa.md/sections/Hello%20world?includeSubsections=true"],["GET","/api/v1/entries/a/backlinks"],["GET","/api/v1/entries/a/outlinks"],["GET","/api/v1/entries/a/related?limit=5"]]);
});

test("pagination snapshots filters and ignores page-local total",async t=>{
  const offsets=[];
  const baseUrl=await server(t,(req,res)=>{
    const q=new URL(req.url,"http://localhost").searchParams;assert.equal(q.get("project"),"original");
    const offset=Number(q.get("offset"));offsets.push(offset);
    res.end(JSON.stringify({entries:offset<2?[{id:String(offset)}]:[],offset,limit:1,total:1}));
  });
  const c=new BrainClient({baseUrl});t.after(()=>c.close());const q={project:"original",limit:1};
  assert.equal(typeof c.entries.iterate,"function");
  const seq=c.entries.iterate(q);q.project="changed";const ids=[];for await(const e of seq)ids.push(e.id);
  assert.deepEqual(ids,["0","1"]);assert.deepEqual(offsets,[0,1,2]);
});

test("pagination rejects truncated pages and stops on consumer break",async t=>{
  let calls=0;let truncated=true;
  const baseUrl=await server(t,(_req,res)=>{calls++;res.end(JSON.stringify({entries:[{id:"a"}],offset:0,limit:100,truncated}));});
  const c=new BrainClient({baseUrl});t.after(()=>c.close());
  assert.equal(typeof c.entries.iterate,"function");
  await assert.rejects(async()=>{for await(const _e of c.entries.iterate()){}},e=>e.code==="pagination_incomplete");
  truncated=false;for await(const _e of c.entries.iterate())break;assert.equal(calls,2);
});

test("pagination rejects NaN rather than silently selecting a default",async t=>{
  let calls=0;const baseUrl=await server(t,(_req,res)=>{calls++;res.end('{"entries":[],"offset":0,"limit":100}');});
  const c=new BrainClient({baseUrl});t.after(()=>c.close());
  await assert.rejects(async()=>{for await(const _e of c.entries.iterate({limit:NaN})){}},e=>e.code==="invalid_pagination");
  assert.equal(calls,0);
});

test("task waiting and blocked routes",async t=>{
  const seen=[];const baseUrl=await server(t,(req,res)=>{seen.push(req.method+" "+req.url);res.end('{"tasks":[]}');});
  const c=new BrainClient({baseUrl});t.after(()=>c.close());
  assert.deepEqual(await c.tasks.waiting("p q"),{tasks:[]});
  assert.deepEqual(await c.tasks.blocked("p q"),{tasks:[]});
  assert.deepEqual(seen,["GET /api/v1/tasks/p%20q/waiting","GET /api/v1/tasks/p%20q/blocked"]);
});

test("task ready and next preserve selection filters",async t=>{
  const seen=[];const baseUrl=await server(t,(req,res)=>{seen.push(req.url);res.end('{"tasks":[],"id":"one"}');});
  const c=new BrainClient({baseUrl});t.after(()=>c.close());
  const filters={feature_id:["a,b","c & d"],executors:"opencode,pi",runner_id:"r one",generated_by_prefix:"automation:foo"};
  await c.tasks.ready("p q",filters);assert.equal((await c.tasks.next("p q",filters)).id,"one");
  await c.tasks.ready("p");await c.tasks.next("p");
  const q="?feature_id=a%2Cb&feature_id=c+%26+d&executors=opencode%2Cpi&runner_id=r+one&generated_by_prefix=automation%3Afoo";
  assert.deepEqual(seen,["/api/v1/tasks/p%20q/ready"+q,"/api/v1/tasks/p%20q/next"+q,"/api/v1/tasks/p/ready","/api/v1/tasks/p/next"]);
});

test("project and observability reads preserve queries",async t=>{
  const seen=[];const baseUrl=await server(t,(req,res)=>{seen.push(req.url);res.end('null');});
  const c=new BrainClient({baseUrl});t.after(()=>c.close());
  await c.projects.list();await c.observability.stats({projects:"a,b",global:true});
  assert.equal(await c.observability.stale({project:"p q",type:"note",days:7,limit:8}),null);
  assert.equal(await c.graph.orphans({project:"p q",type:"note",limit:9}),null);
  assert.deepEqual(seen,["/api/v1/tasks","/api/v1/stats?projects=a%2Cb&global=true","/api/v1/stale?project=p+q&type=note&days=7&limit=8","/api/v1/orphans?project=p+q&type=note&limit=9"]);
});

test("task and feature read routes preserve request and nullable responses",async t=>{
  const seen=[];let body;
  const baseUrl=await server(t,async(req,res)=>{
    seen.push(req.method+" "+req.url);
    if(req.method==="POST"){let raw="";for await(const chunk of req)raw+=chunk;body=JSON.parse(raw);}
    res.end('{"tasks":[],"allCompleted":false,"notFound":["missing"],"complete_on_idle":null,"claimed":false,"features":null,"feature":{"featureId":"f & q","tasks":null,"ready":false}}');
  });
  const c=new BrainClient({baseUrl});t.after(()=>c.close());
  const status=await c.tasks.status("p q",{taskIds:["one","missing"]});
  assert.equal(status.allCompleted,false);assert.deepEqual(status.notFound,["missing"]);
  assert.deepEqual(body,{taskIds:["one","missing"]});
  assert.equal((await c.tasks.metadata("p q","one & two")).complete_on_idle,null);
  assert.equal((await c.tasks.claimStatus("p q","one & two")).claimed,false);
  assert.equal((await c.features.list("p q")).features,null);
  assert.equal((await c.features.ready("p q")).features,null);
  assert.equal((await c.features.get("p q","f & q")).feature.featureId,"f & q");
  assert.deepEqual(seen,["POST /api/v1/tasks/p%20q/status","GET /api/v1/tasks/p%20q/one%20%26%20two/metadata","GET /api/v1/tasks/p%20q/one%20%26%20two/claim-status","GET /api/v1/tasks/p%20q/features","GET /api/v1/tasks/p%20q/features/ready","GET /api/v1/tasks/p%20q/features/f%20%26%20q"]);
});

test("query arrays preserve repeated parameter values without comma coercion",async t=>{
  const seen=[];const baseUrl=await server(t,(req,res)=>{seen.push(req.url);res.end('{"entries":[]}');});
  const c=new BrainClient({baseUrl});t.after(()=>c.close());
  // Exercise the shared transport before binding an array-bearing namespace.
  // JS callers can supply additional query properties; this tests encoding,
  // not admission of these fields by the entries service.
  await c.entries.list({feature_id:["a,b","c & d"],unused:undefined,empty:[],limit:2,global:false});
  assert.equal(seen[0],"/api/v1/entries?feature_id=a%2Cb&feature_id=c+%26+d&limit=2&global=false");
});

test("runner, dispatch, scheduler and placement routes send no bodies",async t=>{
 const seen=[];const baseUrl=await server(t,async(req,res)=>{let body="";for await(const c of req)body+=c;seen.push(`${req.method} ${req.url} ${req.headers["content-type"]??""}${body}`);res.end("{}");});
 const c=new BrainClient({baseUrl});t.after(()=>c.close());
 await c.runners.status();await c.runners.list();await c.runners.get("r 1/x");await c.runners.instances("r1");await c.runners.allInstances();
 await c.dispatch.pauseAll();await c.dispatch.resumeAll();await c.dispatch.pauseProject("p q");await c.dispatch.resumeProject("p");
 await c.dispatch.pauseFeature("p","f/1");await c.dispatch.resumeFeature("p","f1");await c.dispatch.pauseProjectAutomations("p");await c.dispatch.resumeProjectAutomations("p");
 await c.tasks.dispatchLease("p","t");await c.tasks.placementReasons("p","t/2");await c.scheduler.status();
 assert.deepEqual(seen,["GET /api/v1/tasks/runner/status ","GET /api/v1/runners ","GET /api/v1/runners/r%201%2Fx ","GET /api/v1/runners/r1/instances ","GET /api/v1/instances ",
  "POST /api/v1/tasks/runner/pause ","POST /api/v1/tasks/runner/resume ","POST /api/v1/tasks/runner/pause/p%20q ","POST /api/v1/tasks/runner/resume/p ",
  "POST /api/v1/tasks/runner/features/pause/p/f%2F1 ","POST /api/v1/tasks/runner/features/resume/p/f1 ","POST /api/v1/tasks/runner/automations/pause/p ","POST /api/v1/tasks/runner/automations/resume/p ",
  "GET /api/v1/tasks/p/t/dispatch-lease ","GET /api/v1/tasks/p/t%2F2/placement-reasons ","GET /api/v1/scheduler/status "]);
});

test("control routes return proxied instance JSON, null for 204, and server errors",async t=>{
 const seen=[];let mode="ok";
 const baseUrl=await server(t,async(req,res)=>{let body="";for await(const c of req)body+=c;seen.push(`${req.method} ${req.url} ${body}`);
  if(mode==="bad"){res.end("not json");return;}
  if(mode==="err"){res.writeHead(502).end('{"error":"Bad Gateway","message":"runner bridge not connected"}');return;}
  if(req.url.endsWith("/prompt")){res.writeHead(204).end();return;}
  if(req.url.endsWith("/abort")||req.url.includes("/permissions/")){res.end("true");return;}
  if(req.method==="POST"){res.writeHead(201).end('{"success":true,"instance":{"instance_id":"i9","runner_id":"r","kind":"adhoc","status":"starting"}}');return;}
  res.end('{"success":true}');});
 const c=new BrainClient({baseUrl});t.after(()=>c.close());
 assert.equal(await c.control.sendPrompt("r","i","s/1",{text:"hi",model:{providerID:"p",modelID:"m"}}),null);
 assert.equal(await c.control.abortSession("r","i","s"),true);
 assert.equal(await c.control.respondPermission("r","i","s","per 1",{response:"reject"}),true);
 assert.equal((await c.control.spawnInstance("r",{workdir:"/w"})).instance.instance_id,"i9");
 assert.equal((await c.control.killInstance("r","i9")).success,true);
 assert.deepEqual(seen,['POST /api/v1/control/runners/r/instances/i/sessions/s%2F1/prompt {"text":"hi","model":{"providerID":"p","modelID":"m"}}','POST /api/v1/control/runners/r/instances/i/sessions/s/abort ','POST /api/v1/control/runners/r/instances/i/sessions/s/permissions/per%201 {"response":"reject"}','POST /api/v1/control/runners/r/instances {"workdir":"/w"}','DELETE /api/v1/control/runners/r/instances/i9 ']);
 mode="bad";await assert.rejects(c.control.abortSession("r","i","s"),e=>e instanceof BrainError&&e.code==="invalid_response");
 mode="err";await assert.rejects(c.control.sendPrompt("r","i","s",{text:"x"}),e=>e instanceof BrainError&&e.status===502&&e.serverMessage==="runner bridge not connected");
 await assert.rejects(c.control.killInstance("r",".."),e=>e.code==="invalid_request");
});

test("step-3 operator and supervision routes, verbatim command documents and errors",async t=>{
 const seen=[];let mode="ok";
 const baseUrl=await server(t,async(req,res)=>{let body="";for await(const c of req)body+=c;seen.push(`${req.method} ${req.url} ${body}`);
  if(mode==="conflict"){res.writeHead(409).end('{"error":"Stale diff","message":"fetch a fresh diff before reconciling"}');return;}
  if(req.method==="POST"&&req.url==="/api/v1/monitors"){res.writeHead(201).end('{"id":"m","path":"p","title":"t"}');return;}
  if(req.url.endsWith("/reconcile")){res.writeHead(202).end('{"command_id":"c","status":"queued","note":"n"}');return;}
  res.end("{}");});
 const c=new BrainClient({baseUrl});t.after(()=>c.close());
 assert.equal((await c.monitors.create({template_id:"dream",scope_type:"project",project:"p",feature_id:""})).id,"m");
 await c.monitors.deleteByScope({templateId:"dream",scope:{type:"feature",project:"p",feature_id:"f 1"}});
 await c.tasks.runnerCandidates("p","t/1");await c.tasks.proposedRunnerCandidates("p q",{requires_capability:["gpu"]});await c.features.runnerCandidates("p","f/1");
 await c.clientContext.resolve({client:{client_id:"c",host_id:"h"},workspace:{path:""}});
 await c.sync.devices();await c.sync.diff("d 1","o/1");assert.equal((await c.sync.reconcile("d 1","o",{snapshot:"s",action:"discard",raw:""})).status,"queued");
 await c.control.sessionTail("r 1","s/1",{after:"c u",limit:5,max_bytes:2048});await c.control.sessionDescendants("r","s",{limit:20});
 await c.supervision.capabilities();await c.supervision.snapshot({project_id:"p q",limit:50});await c.supervision.dispatchPreview({project_id:"p",task_id:"t",manual:true});
 await c.supervision.submitOperation({id:"op-12345678",operation:"trigger",bogus:1});await c.supervision.getOperation("a b/c");
 await c.supervision.checkpoints({project:"p",id:"chk 1"});await c.supervision.updateCheckpoint({action:"request",checkpoint:{id:"x"},extra:true});
 await c.supervision.budget({project:"p",id:"b"});await c.supervision.updateBudget({action:"reserve",units:1.5});
 assert.deepEqual(seen,[
  'POST /api/v1/monitors {"template_id":"dream","scope_type":"project","project":"p","feature_id":""}',
  'DELETE /api/v1/monitors/by-scope {"templateId":"dream","scope":{"type":"feature","project":"p","feature_id":"f 1"}}',
  'GET /api/v1/tasks/p/t%2F1/runner-candidates ','POST /api/v1/tasks/p%20q/runner-candidates {"requires_capability":["gpu"]}','GET /api/v1/tasks/p/features/f%2F1/runner-candidates ',
  'POST /api/v1/context/resolve {"client":{"client_id":"c","host_id":"h"},"workspace":{"path":""}}',
  'GET /api/v1/sync/devices ','GET /api/v1/sync/devices/d%201/operations/o%2F1/diff ','POST /api/v1/sync/devices/d%201/operations/o/reconcile {"snapshot":"s","action":"discard","raw":""}',
  'GET /api/v1/control/runners/r%201/sessions/s%2F1/tail?after=c+u&limit=5&max_bytes=2048 ','GET /api/v1/control/runners/r/sessions/s/descendants?limit=20 ',
  'GET /api/v1/supervision/capabilities ','GET /api/v1/supervision/snapshot?project_id=p+q&limit=50 ','GET /api/v1/supervision/dispatch-preview?project_id=p&task_id=t&manual=true ',
  'POST /api/v1/supervision/operations {"id":"op-12345678","operation":"trigger","bogus":1}','GET /api/v1/supervision/operations/a%20b%2Fc ',
  'GET /api/v1/supervision/checkpoints?project=p&id=chk+1 ','POST /api/v1/supervision/checkpoints {"action":"request","checkpoint":{"id":"x"},"extra":true}',
  'GET /api/v1/supervision/budgets?project=p&id=b ','POST /api/v1/supervision/budgets {"action":"reserve","units":1.5}']);
 mode="conflict";await assert.rejects(c.sync.reconcile("d","o",{snapshot:"s",action:"discard"}),e=>e instanceof BrainError&&e.code==="conflict"&&e.status===409&&e.serverMessage==="fetch a fresh diff before reconciling");
 await assert.rejects(c.supervision.getOperation(".."),e=>e.code==="invalid_request");
});
