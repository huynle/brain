import test from 'node:test';
import assert from 'node:assert/strict';
import {BrainClient, BrainError} from '../dist/index.js';

const manifest={contract_version:'1.0.0',operations:['health.get'],scripts:{compiled:false,configured:false,deployment_available:false,caller_authorized:false,available:false}};
test('public discovery preserves independent dimensions and rejects inconsistent availability',async()=>{
  for(let mask=0;mask<32;mask++) {
    const scripts={compiled:!!(mask&1),configured:!!(mask&2),deployment_available:!!(mask&4),caller_authorized:!!(mask&8),available:!!(mask&16)};
    const c=new BrainClient({baseUrl:'https://brain.invalid',fetch:async()=>new Response(JSON.stringify({...manifest,scripts}))});
    if(scripts.available===((mask&15)===15))assert.deepEqual((await c.capabilities()).scripts,scripts);
    else await assert.rejects(c.capabilities(),e=>e.code==='invalid_capability_manifest');
    c.close();
  }
});
test('public discovery negotiates on the authenticated immutable transport',async()=>{
  let calls=0;
  const c=new BrainClient({baseUrl:'https://brain.invalid',token:'secret',fetch:async(url,opts)=>{
    calls++;assert.equal(url,'https://brain.invalid/api/v1/capabilities');assert.equal(opts.headers.get('Authorization'),'Bearer secret');
    return new Response(JSON.stringify(manifest));
  }});
  assert.equal(typeof c.capabilities,'function');
  assert.deepEqual(await c.capabilities(),manifest);assert.equal(calls,1);
  c.close();await assert.rejects(c.capabilities());assert.equal(calls,1);
});

test('public discovery refuses old/auth/malformed servers without fallback',async()=>{
  for(const [status,body,code] of [
    [201,JSON.stringify(manifest),'capability_discovery_unavailable'],
    [404,'secret','unsupported_server'],[501,'secret','unsupported_server'],
    [401,'secret','capability_auth_required'],[403,'secret','capability_auth_required'],
    [503,'secret','capability_discovery_unavailable'],
    [200,JSON.stringify({...manifest,contract_version:'0.1.0'}),'incompatible_contract_version'],
    [200,JSON.stringify(manifest).replace('"compiled":false','"compiled":false,"compiled":false'),'invalid_capability_manifest'],
    [200,JSON.stringify({...manifest,principal:'secret'}),'invalid_capability_manifest'],
    [200,' '.repeat(65537),'invalid_capability_manifest'],
  ]) {
    let calls=0;
    const c=new BrainClient({baseUrl:'https://brain.invalid',fetch:async()=>{calls++;return new Response(body,{status});}});
    assert.equal(typeof c.capabilities,'function');
    await assert.rejects(c.capabilities(),e=>e instanceof BrainError&&e.code===code&&e.serverMessage==='');
    assert.equal(calls,1);c.close();
  }
});
