// Install the built SDK package before running this external application.
import { BrainClient } from "@huynle/brain-sdk";

const client = new BrainClient({baseUrl:process.env.BRAIN_API_URL, token:process.env.BRAIN_API_TOKEN});
let id;
try {
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
  await client.entries.bulkUpdate({entries:[{path:updated.path,updates:{title:"Node SDK updated"}}],dry_run:true});
  await client.entries.bulkDelete({paths:[updated.path],dry_run:true});
  await client.entries.move(id,{project:"sdk-example-moved"});
  console.log(JSON.stringify({ok:true,client:"node",id,task_title:task.title}));
} finally {
  try { if (id) await client.entries.delete(id); }
  finally { client.close(); }
}
