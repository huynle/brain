import { chromium, expect } from "@playwright/test";
import assert from "node:assert/strict";
const origin = process.env.BRAIN_MOBILE_TEST_URL ?? "http://localhost:3340";
assert.ok(["localhost", "127.0.0.1"].includes(new URL(origin).hostname));
const browser = await chromium.launch();
try {
  for (const width of [320, 390]) {
    const context = await browser.newContext({viewport:{width,height:844},isMobile:true,hasTouch:true,serviceWorkers:"block"});
    await context.addInitScript(() => {
      localStorage.setItem("brain.mobile.assistantHome", "true");
      localStorage.setItem("panes-v2:assistant-chat:v1", JSON.stringify({version:1,state:{turns:Array.from({length:40},(_,i)=>({role:i%2?'assistant':'user',content:`Message ${i}: This is a seeded conversation for checking mobile history.`,tools:[]})),history:[]}}));
      window.SpeechRecognition = class {
        start() { window.testRecognition = this; }
        stop() { this.onend?.(); }
        abort() { window.micAborted = true; this.onend?.(); }
      };
    });
    const page = await context.newPage();
    await page.goto(origin);
    const button = page.getByRole('button',{name:'Assistant',exact:true});
    await expect(button).toBeVisible({timeout:30000});
    await expect(page.locator('.assistant-panel')).toHaveCount(0);
    const rect = await button.boundingBox();
    assert(Math.abs(rect.x+rect.width/2-width/2)<2 && rect.y>700 && rect.width===rect.height);
    await button.tap();
    await expect(page.locator('.assistant-panel')).toBeVisible();
    await expect(page.getByRole('textbox',{name:'Message to Assistant'})).toBeInViewport();
    await expect(page.locator('.assistant-msg')).toHaveCount(40);
    const history = page.getByRole('log',{name:'Chat history'});
    assert(await history.evaluate(el=>el.scrollHeight>el.clientHeight));
    await history.evaluate(el=>{el.scrollTop=0;el.dispatchEvent(new Event('scroll'));});
    const user = await page.locator('.assistant-msg.user').first().boundingBox();
    const assistant = await page.locator('.assistant-msg.assistant').first().boundingBox();
    assert(user.x>assistant.x);
    await page.getByRole('textbox',{name:'Message to Assistant'}).fill('Existing draft.');
    await page.getByRole('button',{name:'Use microphone'}).tap();
    await expect(page.getByRole('button',{name:'Stop microphone'})).toBeVisible();
    await page.evaluate(()=>window.testRecognition.onresult({results:[[{transcript:'Hello from the microphone'}]]}));
    await expect(page.getByRole('textbox',{name:'Message to Assistant'})).toHaveValue('Existing draft. Hello from the microphone');
    await expect(page.getByRole('button',{name:/^Send/})).toBeDisabled();
    await page.getByRole('button',{name:'Stop microphone'}).tap();
    await expect(page.getByRole('button',{name:/^Send/})).toBeEnabled();
    assert(await history.evaluate(el=>el.scrollTop)<5);
    await page.getByRole('button',{name:'Use microphone'}).tap();
    await page.getByRole('button',{name:'Close assistant',exact:true}).tap();
    assert(await page.evaluate(()=>window.micAborted));
    await button.tap();
    await expect(page.getByRole('button',{name:'Use microphone'})).toBeVisible();
    await page.getByRole('button',{name:'Use microphone'}).tap();
    await page.evaluate(()=>window.testRecognition.onerror({error:'not-allowed'}));
    await expect(page.getByText(/Microphone permission was denied/)).toBeVisible();
    await page.locator('.offline-sync-toggle').tap();
    await expect(page.getByRole('dialog',{name:'Offline sync'})).toBeVisible();
    await page.keyboard.press('Escape');
    await page.screenshot({path:`/tmp/brain-mobile-chat-${width}.png`});
    console.log(`PASS ${width}px: dashboard startup, centered button, bubbles, scroll, dictation draft, denial, mic cleanup and sync access`);
    await context.close();
  }
  const desktop = await browser.newPage({viewport:{width:1280,height:900},serviceWorkers:'block'});
  await desktop.goto(origin);
  await expect(desktop.locator('.statusbar')).toBeVisible({timeout:30000});
  await expect(desktop.locator('.assistant-shortcut')).toHaveCount(0);
  console.log('PASS desktop does not show floating mobile button');
} finally { await browser.close(); }
