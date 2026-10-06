import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,existsSync} from 'node:fs';

const path=new URL('../../../internal/sdkcontract/testdata/capability-decoder.js',import.meta.url);
const decode=existsSync(path)?(0,eval)(readFileSync(path,'utf8')):()=>undefined;
const bytes=s=>new TextEncoder().encode(s);
const fixture={contract_version:'0.1.0',operations:['health.get','entries.get'],scripts:{compiled:false,configured:false,deployment_available:false,caller_authorized:false,available:false}};
const error=code=>e=>e.code===code&&e.message===code&&Object.getPrototypeOf(e)===null;

test('inactive compatibility decoder preserves independent availability dimensions',()=>{
  const fields=Object.keys(fixture.scripts);
  for(let bits=0;bits<32;bits++){
    const input=structuredClone(fixture);
    fields.forEach((name,i)=>input.scripts[name]=Boolean(bits&(1<<i)));
    const s=input.scripts,consistent=s.available===(s.compiled&&s.configured&&s.deployment_available&&s.caller_authorized);
    if(consistent) assert.deepEqual(decode(200,bytes(JSON.stringify(input)),'0.1.0'),input);
    else assert.throws(()=>decode(200,bytes(JSON.stringify(input)),'0.1.0'),error('invalid_capability_manifest'));
  }
});

test('inactive compatibility decoder fails closed on old server, auth, version and malformed JSON',()=>{
  const raw=JSON.stringify(fixture);
  for(const [status,want] of [[404,'unsupported_server'],[501,'unsupported_server'],[401,'capability_auth_required'],[403,'capability_auth_required'],[500,'capability_discovery_unavailable'],[204,'capability_discovery_unavailable']]) assert.throws(()=>decode(status,bytes('private diagnostic'),'0.1.0'),error(want));
  assert.throws(()=>decode(200,bytes(raw),'2.0.0'),error('incompatible_contract_version'));
  assert.throws(()=>decode(200,bytes(raw),''),error('invalid_capability_manifest'));
  const cases=['{}','null',raw+'{}',raw.replace('"compiled":false,',''),raw.replace('"compiled":false','"compiled":null'),raw.replace('"compiled":false','"compiled":"false"'),raw.replace('"compiled":false','"compiled":false,"\\u0063ompiled":false'),raw.replace('"contract_version":"0.1.0"','"contract_version":"0.1.0","contract_version":"0.1.0"'),raw.replace('"entries.get"','"health.get"'),raw.replace('"entries.get"','"arbitrary HTTP"'),raw.replace('"operations":["health.get","entries.get"]','"operations":null'),raw.replace('"contract_version"','"resources":[],"contract_version"'),' '.repeat(65537)+raw];
  for(const body of cases) assert.throws(()=>decode(200,bytes(body),'0.1.0'),error('invalid_capability_manifest'));
  assert.throws(()=>decode(200,new Uint8Array([255]),'0.1.0'),error('invalid_capability_manifest'));
});
