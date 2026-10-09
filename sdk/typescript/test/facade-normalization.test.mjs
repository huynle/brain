import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync, existsSync} from 'node:fs';
import ts from 'typescript';

// This is an inactive, pure argument mapper, NOT a broker or a script allowlist.
const file = new URL('../../../internal/scriptexec/testdata/facade-normalization.js', import.meta.url);
const normalize = existsSync(file) ? (0,eval)(readFileSync(file,'utf8')) : () => undefined;
const source = ts.createSourceFile('index.ts', readFileSync(new URL('../src/index.ts',import.meta.url),'utf8'), ts.ScriptTarget.Latest, true);
const signatures = new Map();
const client = source.statements.find(n => ts.isClassDeclaration(n) && n.name.text === 'BrainClient');
assert.ok(client, 'actual public SDK class');
for (const member of client.members) {
  if (ts.isMethodDeclaration(member) && ['health','search','inject'].includes(member.name.getText(source))) signatures.set(member.name.getText(source),member.parameters);
  if (ts.isPropertyDeclaration(member) && member.initializer && ts.isCallExpression(member.initializer) && member.initializer.expression.getText(source)==='Object.freeze') {
    for (const method of member.initializer.arguments[0].properties) signatures.set(`${member.name.getText(source)}.${method.name.getText(source)}`,method.initializer.parameters);
  }
}
// Runner/control/dispatch operations are deliberately absent from the facade:
// dispatch dials are server-wide writes and control.* is code execution on a
// runner host, so no script mapping exists for any of them (reads included).
const runnerControl = ['runners.status','runners.list','runners.get','runners.instances','runners.allInstances',
  'dispatch.pauseAll','dispatch.resumeAll','dispatch.pauseProject','dispatch.resumeProject','dispatch.pauseFeature','dispatch.resumeFeature','dispatch.pauseProjectAutomations','dispatch.resumeProjectAutomations',
  'control.sendPrompt','control.abortSession','control.respondPermission','control.spawnInstance','control.killInstance',
  'tasks.dispatchLease','tasks.placementReasons','scheduler.status'];
// Step-3 hosted-MCP operations are likewise absent: monitors create runnable
// work, supervision submits prompts/resumes/triggers and its ledgers gate
// them, sync queues browser commands, session views read runner hosts, and
// client context writes the registry. None is script-reachable (reads included).
const hostedStep3 = ['monitors.create','monitors.deleteByScope',
  'tasks.runnerCandidates','tasks.proposedRunnerCandidates','features.runnerCandidates',
  'clientContext.resolve','sync.devices','sync.diff','sync.reconcile',
  'control.sessionTail','control.sessionDescendants',
  'supervision.capabilities','supervision.snapshot','supervision.dispatchPreview','supervision.submitOperation','supervision.getOperation',
  'supervision.checkpoints','supervision.updateCheckpoint','supervision.budget','supervision.updateBudget'];
// automations.effective is a read, but x-brain-script is false: no script
// mapping exists for it, so the facade must reject it as unsupported.
const unsupported = new Set(['entries.iterate','events.stream','attachments.upload','attachments.download','automations.effective',...runnerControl,...hostedStep3]);
const operation = name => ({health:'health.get',search:'search.query',inject:'search.inject'})[name] ?? name;
const plain = value => value === undefined ? undefined : JSON.parse(JSON.stringify(value));
const fixedError = code => e => e.code===code && e.message===code && Object.getPrototypeOf(e)===null;

test('pure facade maps each actual SDK positional argument and default without dispatch', () => {
  assert.equal(signatures.size,147);
  for (const name of [...runnerControl,...hostedStep3]) assert.ok(signatures.has(name),`public SDK lacks ${name}`);
  assert.equal(hostedStep3.length,20);
  for (const [name,parameters] of signatures) {
    if (unsupported.has(name)) {
      assert.throws(()=>normalize(name,[]),fixedError('unsupported_operation'),name);
      continue;
    }
    const data = [...parameters].filter(p=>p.name.getText(source)!=='options');
    for (const useDefaults of [false,true]) {
      const args=[], expected={};
      for (const p of data) {
        const key=p.name.getText(source);
        let value;
        if (useDefaults && p.initializer) value=JSON.parse(p.initializer.getText(source));
        else if (useDefaults && p.questionToken) value=undefined;
        else if (p.type?.kind===ts.SyntaxKind.StringKeyword) value=key+' / ü';
        else if (p.initializer?.kind===ts.SyntaxKind.FalseKeyword) value=true;
        else if (p.initializer && ts.isNumericLiteral(p.initializer)) value=7;
        else value={marker:key,nested:[null,true,3,'text']};
        args.push(useDefaults && (p.initializer||p.questionToken) ? undefined : value);
        if (value!==undefined) expected[key]=value;
      }
      assert.deepEqual(plain(normalize(name,args)),{operation:operation(name),arguments:expected},`${name} defaults=${useDefaults}`);
      assert.deepEqual(plain(normalize(name,[...args,undefined])),{operation:operation(name),arguments:expected},`${name} undefined transport slot`);
      assert.throws(()=>normalize(name,[...args,{}]),fixedError('invalid_arguments'),`${name} transport options prohibited`);
      assert.throws(()=>normalize(name,[...args,undefined,undefined]),fixedError('invalid_arguments'),`${name} excess args`);
    }
  }
});

test('pure normalization rejects non-JSON/type confusion without stringifying errors',()=>{
  for(const value of [null,{},[],false,1,Symbol('private'),()=>{}, {toString(){throw 'private'}}]) assert.throws(()=>normalize('entries.get',[value]),fixedError('invalid_arguments'));
  for(const value of [null,[],true,1,'x']) assert.throws(()=>normalize('entries.create',[value]),fixedError('invalid_arguments'));
  for(const value of [null,1,'false',{}]) assert.throws(()=>normalize('entries.delete',['id',value]),fixedError('invalid_arguments'));
  for(const value of [null,-1,0,1.5,NaN,Infinity,'10']) assert.throws(()=>normalize('graph.related',['id',value]),fixedError('invalid_arguments'));
  const cycle={};cycle.self=cycle;
  let inspected=0;
  const accessor={get secret(){inspected++;throw 'private'}};
  for(const value of [{x:undefined},{x:NaN},{x:1n},{x:()=>{}},cycle,accessor,new Date(),new Uint8Array([1])]) assert.throws(()=>normalize('search',[value]),fixedError('invalid_arguments'));
  assert.equal(inspected,0);
  for(const name of [...unsupported,'request','__proto__','entries.constructor']) assert.throws(()=>normalize(name,new Proxy([],{get(){throw 'private'}})),fixedError('unsupported_operation'));
  const original={__proto__:null,content:{text:'before'}};
  const mapped=normalize('entries.create',[original]);original.content.text='after';
  assert.equal(mapped.arguments.request.content.text,'before','detached input');
});
