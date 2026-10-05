// Install the built SDK package before running this external application.
import { BrainClient } from "@huynle/brain-sdk";

const client = new BrainClient({baseUrl:process.env.BRAIN_API_URL, token:process.env.BRAIN_API_TOKEN});
let id;
try {
  const created = await client.entries.create({type:"task",title:"Node SDK example",content:"Created through the public package",project:"sdk-example"});
  id = created.id;
  const entry = await client.entries.get(id);
  if (!entry.revision) throw new Error("missing revision");
  const updated = await client.entries.update(id,{title:"Node SDK updated",expected_revision:entry.revision});
  if (updated.title !== "Node SDK updated") throw new Error("update not visible");
  const task = await client.tasks.get("sdk-example",id);
  if (task.id !== id) throw new Error("task identity mismatch");
  await client.tasks.list("sdk-example");
  await client.entries.list({project:"sdk-example"});
  console.log(JSON.stringify({ok:true,client:"node",id,task_title:task.title}));
} finally {
  try { if (id) await client.entries.delete(id); }
  finally { client.close(); }
}
