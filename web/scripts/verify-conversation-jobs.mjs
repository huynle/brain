import {chromium,expect} from '@playwright/test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
const base=process.env.BRAIN_PREVIEW_URL||'http://localhost:3340';
const token=process.env.BRAIN_TEST_TOKEN_FILE?JSON.parse(fs.readFileSync(process.env.BRAIN_TEST_TOKEN_FILE,'utf8')).token:'';
const browser=await chromium.launch();
const makeContext=async()=>{const c=await browser.newContext({viewport:{width:390,height:844},serviceWorkers:'block'});if(token)await c.addInitScript(t=>{localStorage.setItem('brain.access_token',t);localStorage.setItem('brain.auth_mode','manual');},token);return c;};
const get=async path=>{const r=await fetch(base+path+"&include_history=true",{headers:token?{Authorization:'Bearer '+token}:{}});assert.equal(r.status,200);return r.json();};
try{
 const context=await makeContext(),page=await context.newPage();
 await page.goto(base);await page.getByRole('button',{name:'Assistant',exact:true}).click();
 const input=page.getByRole('textbox',{name:'Message to Assistant'});
 const label='Background check '+Date.now();
 await input.fill(label+': Find the Start here mobile playground note in mobile-playground, read it, and report its exact path plus a short summary. Use a background job.');
 const response=page.waitForResponse(r=>r.url().endsWith('/assistant/chat/stream'));
 await input.press('Enter');const reply=await response;await reply.finished();
 const id=await page.getByRole('combobox',{name:'Conversation',exact:true}).inputValue();
 let snapshot=await get('/api/v1/assistant/jobs?conversation_id='+encodeURIComponent(id));
 assert.equal(snapshot.jobs.length,1,'one delegated job');
 const jobID=snapshot.jobs[0].id;console.log('Worker state when browser closes:',snapshot.jobs[0].state);
 await context.close();
 const deadline=Date.now()+90000;
 while(Date.now()<deadline){snapshot=await get('/api/v1/assistant/jobs?conversation_id='+encodeURIComponent(id));if(snapshot.jobs[0].state==='completed')break;if(['failed','paused','cancelled'].includes(snapshot.jobs[0].state))throw new Error(JSON.stringify(snapshot.jobs[0]));await new Promise(r=>setTimeout(r,1000));}
 assert.equal(snapshot.jobs[0].state,'completed');assert.equal(snapshot.jobs[0].id,jobID);assert(snapshot.jobs[0].revision>snapshot.jobs[0].acknowledged);
 const fresh=await makeContext(),reopened=await fresh.newPage();
 await reopened.goto(base);await reopened.getByRole('button',{name:'Assistant',exact:true}).click();
 await expect(reopened.getByRole('combobox',{name:'Conversation',exact:true}).locator('option[value="'+id+'"]')).toHaveCount(1,{timeout:15000});
 await reopened.getByRole('combobox',{name:'Conversation',exact:true}).selectOption(id);
 await expect(reopened.locator('.assistant-msg.user')).toContainText(label);
 await expect.poll(async()=>{const s=await get('/api/v1/assistant/jobs?conversation_id='+id);return s.jobs[0].acknowledged===s.jobs[0].revision;},{timeout:60000}).toBe(true);
 await expect(reopened.locator('.assistant-msg.assistant').last()).not.toBeEmpty();
 const delivered=await get('/api/v1/assistant/jobs?conversation_id='+id);
 assert(delivered.conversations.find(c=>c.id===id).history.some(h=>h.role==='assistant'&&h.content.includes('mobile-playground')));
 await reopened.getByRole('button',{name:'New chat',exact:true}).click();
 await expect(reopened.locator('.assistant-msg')).toHaveCount(0);
 await fresh.close();console.log('PASS server worker survives closed browser; fresh device restores session, delivers inbox once, and isolates a new conversation');
}finally{await browser.close();}
