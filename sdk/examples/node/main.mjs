// Install the built SDK package before running this external application.
import { BrainClient, BrainError } from "@huynle/brain-sdk";
import { createServer } from "node:http";

const client = new BrainClient({baseUrl:process.env.BRAIN_API_URL, token:process.env.BRAIN_API_TOKEN});
let id;
try {
  let validation;try{await client.entries.create({});}catch(e){validation=e;}
  if(!(validation instanceof BrainError)||validation.status!==400||!validation.details.length)throw new Error("missing field validation details");
  await client.health();
  if(process.env.BRAIN_SDK_EXTRACTION_FIXTURE==="1") {
    const bytes=Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=","base64");
    const image=await client.attachments.upload("sdk-node-extraction",{filename:"pixel.png",contentType:"image/png",content:bytes});
    try {
      await client.attachments.extract("sdk-node-extraction",image.attachment.id,{});
      if(await client.attachments.text("sdk-node-extraction",image.attachment.id)!=="fixture extracted text")throw new Error("stored extraction text mismatch");
      if(await client.attachments.text("sdk-node-extraction",image.attachment.id)!=="fixture extracted text")throw new Error("repeated stored text mismatch");
    }finally{await client.attachments.delete("sdk-node-extraction",image.attachment.id);}
  }
  if(process.env.BRAIN_SDK_ACTION_FIXTURE==="1") {
    const received=[];
    const receiver=createServer(async(req,res)=>{
      try{let body="";for await(const chunk of req){body+=chunk;if(body.length>8192)throw new Error("oversized test delivery");}received.push(JSON.parse(body));res.writeHead(204);res.end();}
      catch{res.writeHead(400);res.end();}
    });
    await new Promise((resolve,reject)=>{receiver.once("error",reject);receiver.listen(0,"127.0.0.1",resolve);});
    let webhook;
    try {
      webhook=await client.webhooks.create({name:"Node fixture",url:`http://127.0.0.1:${receiver.address().port}`,events:["webhook.test"],enabled:false});
      if((await client.webhooks.list(true)).webhooks.some(w=>w.id===webhook.id))throw new Error("disabled webhook in enabled list");
      await client.webhooks.update(webhook.id,{name:"Node fixture updated",enabled:true});
      if((await client.webhooks.get(webhook.id)).name!=="Node fixture updated")throw new Error("webhook update mismatch");
      if(!(await client.webhooks.list(true)).webhooks.some(w=>w.id===webhook.id))throw new Error("enabled webhook missing");
      if(!(await client.webhooks.test(webhook.id)).success)throw new Error("webhook test failed");
      if(received.length!==1||received[0].type!=="webhook.test")throw new Error("local receiver mismatch");
      if((await client.webhooks.deliveries(webhook.id,10)).deliveries.length!==1)throw new Error("delivery history mismatch");
      await client.webhooks.delete(webhook.id);
      let missing;try{await client.webhooks.get(webhook.id);}catch(e){missing=e;}
      if(!(missing instanceof BrainError)||missing.status!==404)throw new Error("deleted webhook still readable");
      webhook=undefined;
    }finally{try{if(webhook)await client.webhooks.delete(webhook.id);}finally{await new Promise(resolve=>receiver.close(resolve));}}
    const automation=await client.entries.create({type:"automation",project:"sdk-node-automation",title:"Node manual action",content:"local fixture",status:"active",trigger:{type:"cron",schedule:"0 5 * * *"},action:{type:"prompt",direct_prompt:"Do the {{.Project}} thing."}});
    let generated;
    try {
      const run=await client.automations.run({path:automation.id});generated=run.task_id;
      if(run.task_ids.length!==1||(await client.entries.get(generated)).generated_by!==`automation:${automation.id}`)throw new Error("automation task provenance mismatch");
      const history=await client.automations.runs({project:"sdk-node-automation",automation_id:automation.id});
      if(!history.entries?.length)throw new Error("automation run history absent");
      for(const entry of history.entries){const audit=await client.automations.getRun(entry.id);if(audit.type!=="automation_run")throw new Error("run audit type mismatch");await client.entries.delete(entry.id);}
    }finally{if(generated)await client.entries.delete(generated);await client.entries.delete(automation.id);}
  }
  const created = await client.entries.create({type:"task",title:"Node SDK example",content:"## Details\nCreated through the public package",project:"sdk-example"});
  id = created.id;
  const entry = await client.entries.get(id);
  if (!entry.revision) throw new Error("missing revision");
  const updated = await client.entries.update(id,{title:"Node SDK updated",expected_revision:entry.revision});
  if (updated.title !== "Node SDK updated") throw new Error("update not visible");
  const task = await client.tasks.get("sdk-example",id);
  const waitingTask=await client.entries.create({type:"task",project:"sdk-example",title:"Node dependency selection",content:"Dependency selection fixture",status:"pending",depends_on:[id]});
  try {
    await client.entries.update(id,{status:"pending"});
    const selection={executors:"opencode",feature_id:["", "not-present"]};
    if(!((await client.tasks.ready("sdk-example",selection)).tasks??[]).some(t=>t.id===id))throw new Error("ready prerequisite missing");
    if((await client.tasks.next("sdk-example",selection)).id!==id)throw new Error("next selected waiting task");
    if(await client.tasks.next("sdk-example",{feature_id:["not-present"]})!==null)throw new Error("missing next did not return null");
    if(!((await client.tasks.waiting("sdk-example")).tasks??[]).some(t=>t.id===waitingTask.id))throw new Error("pending dependency omitted from waiting");
    await client.entries.update(id,{status:"cancelled"});
    if(!((await client.tasks.blocked("sdk-example")).tasks??[]).some(t=>t.id===waitingTask.id))throw new Error("cancelled dependency omitted from blocked");
    if(((await client.tasks.waiting("sdk-example")).tasks??[]).some(t=>t.id===waitingTask.id))throw new Error("hard blocked dependency retained in waiting");
  } finally {await client.entries.delete(waitingTask.id);await client.entries.update(id,{status:"pending"});}
  if (task.id !== id) throw new Error("task identity mismatch");
  if(!(await client.projects.list()).projects.includes("sdk-example"))throw new Error("project catalog missing created task project");
  const stats=await client.observability.stats({project:"sdk-example"});
  if(stats.projectEntries<1||stats.totalEntries<1)throw new Error("scoped statistics missing task");
  if(!((await client.graph.orphans({project:"sdk-example",type:"task",limit:100}))??[]).some(e=>e.id===id))throw new Error("unlinked task omitted from orphans");
  if(!((await client.observability.stale({project:"sdk-example",type:"task",days:30}))??[]).some(e=>e.id===id))throw new Error("unverified task omitted from stale results");
  await client.tasks.list("sdk-example");
  await client.entries.list({project:"sdk-example"});
  let found=false;for await(const entry of client.entries.iterate({project:"sdk-example",limit:1}))if(entry.id===id)found=true;
  if(!found)throw new Error("iterator did not find created entry");
  await client.search({query:"Node SDK updated",strategy:"fts"});
  await client.sections.list(id); await client.sections.get(id,"Details",true);
  await client.graph.backlinks(id); await client.graph.outlinks(id); await client.graph.related(id,5);
  const file=await client.attachments.upload("sdk-example",{filename:"example.txt",content:new TextEncoder().encode("sdk attachment"),contentType:"text/plain"});
  const attachmentID=file.attachment.id;
  await client.attachments.get("sdk-example",attachmentID);await client.attachments.list("sdk-example");
  if(new TextDecoder().decode(await client.attachments.download("sdk-example",attachmentID))!=="sdk attachment")throw new Error("attachment bytes mismatch");
  await client.attachments.attach("sdk-example",id,{attachment:{id:attachmentID,role:"source"}});await client.attachments.forEntry("sdk-example",id);
  await client.attachments.detach("sdk-example",id,attachmentID,"source");await client.attachments.delete("sdk-example",attachmentID);
  const goal=await client.goals.create({project:"sdk-example",title:"Node SDK goal example",config:{id:"",task_id:id},action:{type:"create_task"}});
  await client.goals.update(goal.goal_id,{title:"Node SDK goal updated"});await client.goals.list({project:"sdk-example"});
  await client.goals.progress(goal.goal_id);
  await client.entries.update(id,{status:"completed"});
  const goalRun=await client.goals.run(goal.goal_id);
  if(goalRun.decision!=="complete"||goalRun.generated_task_id)throw new Error("goal reconcile did not complete existing work");
  const goalAudit=await client.goals.audit(goal.goal_id,10);
  if(!goalAudit.audit.some(a=>a.decision==="complete"))throw new Error("goal audit omitted completion");
  await client.goals.delete(goal.goal_id);
  const remindAt=new Date(Date.now()+86_400_000).toISOString();
  const reminder=await client.reminders.create({project:"sdk-example",title:"Node SDK reminder",config:{id:"",action:"notify",remind_at:remindAt}});
  const reminderID=reminder.reminder_id;
  if((await client.reminders.update(reminderID,{title:"Node SDK reminder updated"})).title!=="Node SDK reminder updated")throw new Error("reminder update missing");
  if(!(await client.reminders.list({project:"sdk-example"})).reminders.some(r=>r.reminder_id===reminderID))throw new Error("reminder list missing created item");
  await client.reminders.snooze(reminderID,{remind_at:remindAt});
  const fired=await client.reminders.fire(reminderID);
  if(!fired.fired_at||fired.fire_count!==1||fired.generated_task_id)throw new Error("notify reminder did not fire without generating work");
  if((await client.reminders.get(reminderID)).fire_count!==1)throw new Error("reminder firing not durable");
  await client.reminders.ack(reminderID);
  if(!(await client.reminders.delete(reminderID)).deleted)throw new Error("reminder deletion failed");
  const countsBefore=await client.attention.counts();
  const notice=await client.attention.create({title:"Node SDK attention",kind:"sdk",project:"sdk-example"});
  if(!notice.recipient)throw new Error("missing recipient binding");
  if((await client.attention.get(notice.id)).title!=="Node SDK attention")throw new Error("attention read mismatch");
  if(!(await client.attention.list({project:"sdk-example"})).attention.some(a=>a.id===notice.id))throw new Error("attention list missing item");
  if((await client.attention.counts()).unread!==countsBefore.unread+1)throw new Error("unread count mismatch");
  await client.attention.read(notice.id);
  if((await client.attention.counts()).unread!==countsBefore.unread)throw new Error("read state not reflected in count");
  await client.attention.unread(notice.id);await client.attention.snooze(notice.id,{snoozed_until:remindAt});
  await client.attention.resolve(notice.id);await client.attention.dismiss(notice.id);
  await client.entries.bulkUpdate({entries:[{path:updated.path,updates:{title:"Node SDK updated"}}],dry_run:true});
  await client.entries.bulkDelete({paths:[updated.path],dry_run:true});
  await client.entries.move(id,{project:"sdk-example-moved"});
  console.log(JSON.stringify({ok:true,client:"node",id,task_title:task.title}));
} finally {
  try { if (id) await client.entries.delete(id); }
  finally { client.close(); }
}
