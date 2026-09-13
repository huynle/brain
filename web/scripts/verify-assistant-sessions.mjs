import {chromium,expect} from '@playwright/test';
import assert from 'node:assert/strict';
const browser=await chromium.launch();
try{
 const context=await browser.newContext({viewport:{width:390,height:844},serviceWorkers:'block'});
 const page=await context.newPage();const requests=[];
 await page.route('**/api/v1/assistant/chat/stream',route=>{requests.push(route.request().postDataJSON());return route.fulfill({contentType:'application/x-ndjson',body:'{"type":"done","reply":"Saved reply"}\n'});});
 await page.goto('http://localhost:3340');await page.getByRole('button',{name:'Assistant',exact:true}).click();
 const message=page.getByRole('textbox',{name:'Message to Assistant'});
 await message.fill('First topic');await message.press('Enter');await expect(page.getByText('Saved reply',{exact:true})).toBeVisible();
 const first=await page.getByRole('combobox',{name:'Conversation',exact:true}).inputValue();
 await page.getByRole('button',{name:'New chat',exact:true}).click();
 await message.fill('Second topic');await message.press('Enter');await expect(page.getByText('Saved reply',{exact:true})).toBeVisible();
 assert.equal(requests[1].history.length,0);
 await page.getByRole('combobox',{name:'Conversation',exact:true}).selectOption(first);
 await expect(page.locator('.assistant-msg.user')).toContainText('First topic');
 await page.reload();await page.getByRole('button',{name:'Assistant',exact:true}).click();
 await expect(page.locator('.assistant-msg.user')).toContainText('First topic');
 console.log('PASS saved conversations retain isolated history across switches and reloads');
}finally{await browser.close();}
