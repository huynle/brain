// INACTIVE PURE FIXTURE. This expression returns a positional argument mapper.
// It is not installed in the native brain object, has no dispatcher, and grants
// no script support. DTO/service/preflight/authority validation is still required.
(() => {
  const create = Object.create, keys = Object.keys, descriptors = Object.getOwnPropertyDescriptors;
  const proto = Object.getPrototypeOf, array = Array.isArray, finite = Number.isFinite;
  const own = Function.prototype.call.bind(Object.prototype.hasOwnProperty);
  const specs = create(null);
  // Positional names match the public TS client. ? means optional; = supplies
  // the TS default. Transport options are never accepted by this fixture.
  const groups = [
    ['health projects.list attention.counts', ''],
    ['search inject entries.create entries.bulkUpdate entries.bulkDelete goals.create webhooks.create automations.run reminders.create attention.create', 'request'],
    ['entries.get sections.list graph.backlinks graph.outlinks goals.delete goals.progress goals.run webhooks.get webhooks.delete webhooks.test automations.getRun reminders.get reminders.delete reminders.ack reminders.fire attention.get attention.read attention.unread attention.resolve attention.dismiss', 'id'],
    ['entries.update entries.updateMetadata entries.move goals.update webhooks.update reminders.update reminders.snooze attention.snooze', 'id request'],
    ['entries.list observability.timeline observability.stats observability.stale events.recent goals.list automations.runs reminders.list attention.list graph.orphans', 'query?'],
    ['events.wait events.resourceHealth', 'query'],
    ['attachments.list tasks.waiting tasks.blocked tasks.list projects.getPlacement features.chains features.list features.ready', 'project'],
    ['attachments.get attachments.delete attachments.text attachments.forEntry tasks.delivery tasks.trigger tasks.metadata tasks.claimStatus tasks.get features.cancel features.get', 'project id'],
    ['attachments.extract attachments.attach tasks.verifyDelivery tasks.resumeWithContext tasks.assign tasks.clearAssignment tasks.dispatch features.resumeWithContext features.assign features.clearAssignment', 'project id request'],
    ['tasks.resume tasks.run features.resume features.checkout features.run', 'project id request={}'],
    ['tasks.logs', 'project id query?'],
    ['tasks.ready tasks.next', 'project query?'],
    ['tasks.status projects.setPlacement', 'project request'],
    ['projects.run', 'project request={}'],
    ['attachments.detach', 'project id attachmentID role'],
    ['entries.delete', 'id force=false'],
    ['projects.delete', 'project confirm force=false'],
    ['sections.get', 'id title includeSubsections=false'],
    ['graph.related', 'id limit=10'],
    ['goals.audit webhooks.deliveries', 'id limit=50'],
    ['webhooks.list', 'enabled=false'],
  ];
  for (const [names, args] of groups) for (const name of names.split(' ')) specs[name] = args ? args.split(' ') : [];
  const aliases = {health:'health.get',search:'search.query',inject:'search.inject'};
  const fail = code => { const e=create(null); e.code=e.message=code; throw e; };
  const bad = () => fail('invalid_arguments');
  // Copy JSON data rather than stringify: getters/toJSON are never evaluated,
  // unsupported values are not silently dropped or coerced into null. Reflection
  // on a Proxy can still execute traps inside the isolated worker, not the host.
  const copy = (value, seen = new Set(), depth = 0) => {
    if (depth > 32) bad();
    if (value === null || typeof value === 'boolean' || typeof value === 'string') return value;
    if (typeof value === 'number') { if (!finite(value)) bad(); return value; }
    if (typeof value !== 'object' || seen.has(value)) bad();
    const isArray=array(value), p=proto(value);
    if (!isArray && p!==Object.prototype && p!==null) bad();
    seen.add(value);
    const ds=descriptors(value), out=isArray ? [] : create(null);
    if (Object.getOwnPropertySymbols(value).length) bad();
    const names=keys(ds);
    if (isArray && names.length !== value.length+1) bad();
    for (const key of names) {
      if (isArray && key==='length') continue;
      const d=ds[key];
      if (!own(d,'value') || !d.enumerable) bad();
      if (isArray && (!/^(0|[1-9][0-9]*)$/.test(key) || Number(key)>=value.length)) bad();
      out[key]=copy(d.value,seen,depth+1);
    }
    seen.delete(value);
    return out;
  };
  return (name, args) => {
    if (typeof name!=='string' || !own(specs,name)) fail('unsupported_operation');
    try {
      const spec=specs[name], out=create(null);
      if (!array(args) || args.length>spec.length+1 || args[spec.length]!==undefined) bad();
      for (let i=0;i<spec.length;i++) {
        const [raw,defaultText]=spec[i].split('='), optional=raw.endsWith('?'), key=optional?raw.slice(0,-1):raw;
        let value=args[i];
        if (value===undefined) {
          if (defaultText!==undefined) value=JSON.parse(defaultText);
          else if (optional) continue;
          else bad();
        }
        if (key==='request' || key==='query') {
          if (value===null || typeof value!=='object' || array(value)) bad();
          value=copy(value);
        } else if (key==='limit') {
          if (!Number.isSafeInteger(value) || value<1) bad();
        } else if (['force','enabled','includeSubsections'].includes(key)) {
          if (typeof value!=='boolean') bad();
        } else if (typeof value!=='string' || value.length===0) bad();
        out[key]=value;
      }
      return {operation:own(aliases,name)?aliases[name]:name, arguments:out};
    } catch { bad(); }
  };
})()
