// INACTIVE proposed client decoder, not a public SDK method or a server grant.
// Consistent claimed availability is information only; execution remains unwired.
(() => {
  const fail=code=>{const e=Object.create(null);e.code=e.message=code;throw e;};
  const invalid=()=>fail('invalid_capability_manifest');
  const exact=(value,names)=>value!==null&&typeof value==='object'&&!Array.isArray(value)&&Object.keys(value).length===names.length&&names.every(k=>Object.hasOwn(value,k));
  return (status,body,expected)=>{
    if(status===404||status===501)fail('unsupported_server');
    if(status===401||status===403)fail('capability_auth_required');
    if(status!==200)fail('capability_discovery_unavailable');
    if(!(body instanceof Uint8Array)||!body.byteLength||body.byteLength>65536||typeof expected!=='string'||!expected||new TextEncoder().encode(expected).length>64)invalid();
    let text,value;
    try{
      text=new TextDecoder('utf-8',{fatal:true,ignoreBOM:true}).decode(body);
      value=JSON.parse(text);
      // JSON.parse checks grammar but discards duplicate keys. Scan its validated
      // token stream separately, comparing decoded keys within each object.
      const tokens=[...text.matchAll(/"(?:\\.|[^"\\])*"|[{}\[\]:,]|true|false|null|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g)].map(m=>m[0]);
      const stack=[];
      for(let i=0;i<tokens.length;i++){
        const token=tokens[i];
        if(token==='{'||token==='['){stack.push(token==='{'?new Set():null);if(stack.length>4)invalid();}
        else if(token==='}'||token===']')stack.pop();
        else if(token.startsWith('"')&&tokens[i+1]===':'){
          const key=JSON.parse(token),seen=stack[stack.length-1];
          if(!seen||seen.has(key))invalid();seen.add(key);
        }
      }
    }catch{invalid();}
    if(!exact(value,['contract_version','operations','scripts'])||typeof value.contract_version!=='string'||!value.contract_version||new TextEncoder().encode(value.contract_version).length>64)invalid();
    if(!Array.isArray(value.operations)||value.operations.length>10000)invalid();
    const seen=new Set();
    for(const op of value.operations){if(typeof op!=='string'||!/^[a-z][a-zA-Z0-9]{0,63}\.[a-z][a-zA-Z0-9]{0,63}$/.test(op)||seen.has(op))invalid();seen.add(op);}
    const fields=['compiled','configured','deployment_available','caller_authorized','available'],s=value.scripts;
    if(!exact(s,fields)||fields.some(k=>typeof s[k]!=='boolean')||s.available!==(s.compiled&&s.configured&&s.deployment_available&&s.caller_authorized))invalid();
    if(value.contract_version!==expected)fail('incompatible_contract_version');
    return value;
  };
})()
