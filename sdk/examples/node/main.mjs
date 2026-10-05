// Install the built SDK package before running this external application.
import { BrainClient, BrainError } from "@huynle/brain-sdk";

const client = new BrainClient({baseUrl:process.env.BRAIN_API_URL, token:process.env.BRAIN_API_TOKEN});
let id;
try {
  let validation;try{await client.entries.create({});}catch(e){validation=e;}
  if(!(validation instanceof BrainError)||validation.status!==400||!validation.details.length)throw new Error("missing field validation details");
  await client.health();
  const created = await client.entries.create({type:"task",title:"Node SDK example",content:"## Details\nCreated through the public package",project:"sdk-example"});
  id = created.id;
  const entry = await client.entries.get(id);
  if (!entry.revision) throw new Error("missing revision");
  const updated = await client.entries.update(id,{title:"Node SDK updated",expected_revision:entry.revision});
  if (updated.title !== "Node SDK updated") throw new Error("update not visible");
  const task = await client.tasks.get("sdk-example",id);
  if (task.id !== id) throw new Error("task identity mismatch");
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
