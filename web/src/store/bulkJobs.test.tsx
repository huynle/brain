import { strict as assert } from "node:assert";
import { test } from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { BulkJobCard } from "../components/common/BulkJobs";
import { submitBulkJob, retrySubmission, refreshBulkJobs, useBulkJobs, type BulkJob } from "./bulkJobs";

const job: BulkJob = {id:"j",request_id:"test",operation:"delete",state:"running",total:1000,pending:800,running:1,succeeded:199,failed:0,uncertain:0,skipped:0};
test("server job exposes real progress, pause, and safe uncertainty controls", () => {
 const html=renderToStaticMarkup(createElement(BulkJobCard,{job}));
 assert.match(html,/max="1000" value="199"/); assert.match(html,/Pause/);
 assert.match(html,/You can close this tab/);
 const uncertain=renderToStaticMarkup(createElement(BulkJobCard,{job:{...job,state:"needs_attention",pending:0,running:0,succeeded:999,uncertain:1}}));
 assert.match(uncertain,/will not be retried automatically/);assert.doesNotMatch(uncertain,/Retry failed entries/);assert.match(uncertain,/View results/);
 const failed=renderToStaticMarkup(createElement(BulkJobCard,{job:{...job,state:"needs_attention",failed:1}}));
 assert.match(failed,/Retry failed entries/);
});
test("lost submission response preserves exact request ID and reconnects without a POST", async () => {
 const memory=new Map<string,string>();
 const previousFetch=globalThis.fetch;
 const descriptor=Object.getOwnPropertyDescriptor(globalThis,"localStorage");
 Object.defineProperty(globalThis,"localStorage",{value:{getItem:(k:string)=>memory.get(k)??null,setItem:(k:string,v:string)=>memory.set(k,v)},configurable:true});
 useBulkJobs.setState({jobs:[],submissions:[]});
 const bodies:string[]=[];
 let stored:BulkJob=job;
 globalThis.fetch=(async (_input,init)=>{
  if(init?.method==="POST") {
   const raw=String(init.body);bodies.push(raw);
   assert.ok(memory.get("brain.bulk-submissions")?.includes(JSON.parse(raw).request_id),"persist before POST");
   stored={...job,request_id:JSON.parse(raw).request_id};
   if(bodies.length===1) throw new Error("lost response");
   return new Response(JSON.stringify(stored),{status:202,headers:{"Content-Type":"application/json"}});
  }
  return new Response(JSON.stringify([stored]),{status:200,headers:{"Content-Type":"application/json"}});
 }) as typeof fetch;
 try {
  await submitBulkJob({operation:"delete",paths:["projects/test/note/a.md"]});
  assert.equal(useBulkJobs.getState().submissions.length,1);
  await retrySubmission(useBulkJobs.getState().submissions[0]);
  assert.equal(bodies.length,2);assert.equal(bodies[0],bodies[1]);
  useBulkJobs.setState({jobs:[]});await refreshBulkJobs();
  assert.equal(useBulkJobs.getState().jobs[0].id,"j");assert.equal(bodies.length,2);
 } finally {
  globalThis.fetch=previousFetch;
  if(descriptor) Object.defineProperty(globalThis,"localStorage",descriptor); else Reflect.deleteProperty(globalThis,"localStorage");
 }
});
