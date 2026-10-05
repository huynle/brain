import test from 'node:test';
import assert from 'node:assert/strict';
import {BrainClient} from '../dist/index.js';

test('metadata, deletion, delivery and finite event routes retain wire semantics',async()=>{
 const cases=[
 ['PATCH','/entries/e%20x/metadata',{expected_revision:'r',title:'new',schedule_enabled:false,tags:[]},{id:'e'},c=>c.entries.updateMetadata('e x',{expected_revision:'r',title:'new',schedule_enabled:false,tags:[]})],
 ['POST','/inject',{query:'q'},{context:'text',entries:null,total:0},c=>c.inject({query:'q'})],
 ['DELETE','/tasks/p%20q?confirm=explicit&force=false',undefined,{deleted:1,failed:1,errors:['failed'],directory_removed:false},c=>c.projects.delete('p q','explicit')],
 ['GET','/tasks/p/t/delivery',undefined,{delivery:null,unmet:null,task_id:'t',implementation_status:'pending'},c=>c.tasks.delivery('p','t')],
 ['POST','/tasks/p/t/delivery',{action:'verify',expected_revision:2},{delivery:{revision:3,verification_error:'unavailable'},unmet:['delivery_evidence']},c=>c.tasks.verifyDelivery('p','t',{action:'verify',expected_revision:2})],
 ['GET','/events/recent?project_id=p&source=api&type=task.*',undefined,{events:null,count:0,coverage:{buffered:10,capacity:100}},c=>c.events.recent({project_id:'p',source:'api',type:'task.*'})],
 ['GET','/events/wait?project_id=p&timeout_ms=0&after=opaque%2Bcursor',undefined,{events:[],next_cursor:'next',cursor_expired:true,timed_out:false,shutdown:false,truncated:true},c=>c.events.wait({project_id:'p',timeout_ms:0,after:'opaque+cursor'})],
 ['GET','/events/resource-health?project_id=p&task_id=t',undefined,{samples:[],availability:'unavailable',warning_fraction:0.8},c=>c.events.resourceHealth({project_id:'p',task_id:'t'})],
 ['GET','/timeline?from=2026-10-01T00%3A00%3A00Z&project=p',undefined,{items:null,warnings:null,truncated:false},c=>c.observability.timeline({from:'2026-10-01T00:00:00Z',project:'p'})],
 ];
 for(const [method,path,body,response,call]of cases){let calls=0;const c=new BrainClient({baseUrl:'https://example.test',fetch:async(url,init)=>{calls++;assert.equal(url,'https://example.test/api/v1'+path);assert.equal(init.method,method);assert.deepEqual(init.body===undefined?undefined:JSON.parse(init.body),body);return Response.json(response);}});assert.deepEqual(await call(c),response);assert.equal(calls,1);c.close();}
});

test('nineteen task, feature and project adapters preserve route, body, options and outcome', async () => {
 const p='p q',id='t x',f='f x',o={requestId:'action-fixture',idempotencyKey:'no-retry'};
 const cases=[
  ['POST','/tasks/p%20q/t%20x/resume',{force:true},{task_id:id,resumed:false,reason:'live claim'},c=>c.tasks.resume(p,id,{force:true},o)],
  ['POST','/tasks/p%20q/t%20x/resume-with-context',{injected_context:'context',prefer_same_session:false},{task_id:id,resumed:true,resume_mode:'rehydrate'},c=>c.tasks.resumeWithContext(p,id,{injected_context:'context',prefer_same_session:false},o)],
  ['PUT','/tasks/p%20q/t%20x/assignment',{runner_id:'r',intent:'assign'},{status:'assigned'},c=>c.tasks.assign(p,id,{runner_id:'r',intent:'assign'},o)],
  ['POST','/tasks/p%20q/t%20x/assignment/clear',{intent:'clear'},{status:'cleared'},c=>c.tasks.clearAssignment(p,id,{intent:'clear'},o)],
  ['POST','/tasks/p%20q/t%20x/trigger',undefined,{success:true,triggered:false,reason:'pending'},c=>c.tasks.trigger(p,id,o)],
  ['POST','/tasks/p%20q/t%20x/run',{force:true},{dispatched:false,reason:'no_online_runner'},c=>c.tasks.run(p,id,{force:true},o)],
  ['POST','/tasks/p%20q/t%20x/dispatch',{targetRunnerId:'r'},{success:true,leaseId:'lease'},c=>c.tasks.dispatch(p,id,{targetRunnerId:'r'},o)],
  ['GET','/tasks/p%20q/t%20x/logs?limit=2&offset=0',undefined,{lines:[],total:3,offset:0,limit:2},c=>c.tasks.logs(p,id,{limit:2,offset:0},o)],
  ['GET','/projects/p%20q/placement',undefined,null,c=>c.projects.getPlacement(p,o)],
  ['PUT','/projects/p%20q/placement',{project_id:'body',affinity:'soft'},{project_id:p,affinity:'soft'},c=>c.projects.setPlacement(p,{project_id:'body',affinity:'soft'},o)],
  ['POST','/tasks/p%20q/run',{},{featuresDispatched:0,featuresSkipped:1},c=>c.projects.run(p,{},o)],
  ['POST','/tasks/p%20q/features/f%20x/resume',{},{results:null,truncated:true,total_results:201,total_skipped:1},c=>c.features.resume(p,f,{},o)],
  ['POST','/tasks/p%20q/features/f%20x/resume-with-context',{injected_context:'context'},{results:[{resumed:true,resume_mode:'live_injected',injected_live:true}]},c=>c.features.resumeWithContext(p,f,{injected_context:'context'},o)],
  ['PUT','/tasks/p%20q/features/f%20x/assignment',{runner_id:'r'},{status:'assigned'},c=>c.features.assign(p,f,{runner_id:'r'},o)],
  ['POST','/tasks/p%20q/features/f%20x/assignment/clear',{intent:'clear'},{status:'cleared'},c=>c.features.clearAssignment(p,f,{intent:'clear'},o)],
  ['POST','/tasks/p%20q/features/f%20x/checkout',{merge_policy:'prompt_only'},{created:false,generatedKey:'key'},c=>c.features.checkout(p,f,{merge_policy:'prompt_only'},o)],
  ['POST','/tasks/p%20q/features/f%20x/run',{includeDependents:true},{dispatched:false,results:null},c=>c.features.run(p,f,{includeDependents:true},o)],
  ['DELETE','/tasks/p%20q/features/f%20x/run',undefined,{cancelled:false,detail:'none queued'},c=>c.features.cancel(p,f,o)],
  ['GET','/tasks/p%20q/chains',undefined,{chains:[]},c=>c.features.chains(p,o)],
 ];
 for(const [method,path,body,response,call] of cases){
  let calls=0;
  const c=new BrainClient({baseUrl:'https://example.test',fetch:async(url,init)=>{
   calls++;assert.equal(url,'https://example.test/api/v1'+path);assert.equal(init.method,method);
   assert.deepEqual(init.body===undefined?undefined:JSON.parse(init.body),body);
   assert.equal(init.headers.get('X-Request-ID'),o.requestId);assert.equal(init.headers.get('Idempotency-Key'),o.idempotencyKey);
   return Response.json(response);
  }});
  assert.deepEqual(await call(c),response);assert.equal(calls,1);c.close();
 }
});
