import test from 'node:test';
import assert from 'node:assert/strict';
import { PROJECT, uptimeConfigs, alertPolicies } from './monitoring_config.mjs';
import { reconcile, containsDesired } from './configure_monitoring.mjs';

function fakeAPI() {
  const data = {notificationChannels:[{name:`projects/${PROJECT}/notificationChannels/1`,type:'email',enabled:true,labels:{email_address:'alerts@example.test'}}],uptimeCheckConfigs:[],alertPolicies:[]};
  const writes=[];
  return {data,writes,list:async kind=>structuredClone(data[kind]),request:async(path,method,body)=>{
    writes.push({path,method,body});
    const kind=path.split('/')[2];
    const result={...body,name:body.name ?? `projects/${PROJECT}/${kind}/${data[kind].length+1}`};
    if (method==='POST') data[kind].push(result);
    else data[kind]=data[kind].map(x=>x.name===result.name?result:x);
    return structuredClone(result);
  }};
}
test('bounded HTTPS GET checks, exact domains, no auth or writes',()=>{
  const configs=uptimeConfigs();
  assert.equal(configs.length,3);
  assert.deepEqual(configs.map(c=>c.monitoredResource.labels.host),['selectosport.com','admin.selectosport.com','api.selectosport.com']);
  for(const c of configs){
    assert.equal(c.httpCheck.requestMethod,'GET');assert.equal(c.httpCheck.validateSsl,true);
    assert.equal(c.period,'300s');assert.equal(c.selectedRegions.length,3);
    assert.equal(c.httpCheck.acceptedResponseStatusCodes[0].statusValue,200);
    assert.equal(c.monitoredResource.labels.project_id,PROJECT);
  }
});
test('all seven policies notify the specified channel and scope existing services',()=>{
  const policies=alertPolicies(uptimeConfigs().map((c,i)=>({...c,name:`checks/${i}`})),'channels/approved');
  assert.equal(policies.length,7);
  for(const p of policies){
    assert.equal(p.enabled,true);assert.deepEqual(p.notificationChannels,['channels/approved']);
    for(const c of p.conditions){
      const condition=c.conditionThreshold ?? c.conditionAbsent ?? c.conditionMatchedLog;
      assert.match(condition.filter,/project_id="destry-development"/);
    }
  }
});
test('dry run never writes',async()=>{
  const api=fakeAPI();const result=await reconcile(api,'alerts@example.test');
  assert.equal(result.mode,'plan');assert.equal(api.writes.length,0);
});
test('apply converges and does not duplicate or update unchanged resources',async()=>{
  const api=fakeAPI();await reconcile(api,'alerts@example.test',true);
  assert.equal(api.writes.length,10);
  assert.equal((await reconcile(api,'alerts@example.test',true)).changes.length,0);
  assert.equal(api.writes.length,10);
});
test('channel must match and be verified/enabled',async()=>{
  for(const mode of ['missing','disabled','unverified']){
    const api=fakeAPI();
    if(mode==='missing')api.data.notificationChannels=[];
    if(mode==='disabled')api.data.notificationChannels[0].enabled=false;
    if(mode==='unverified')api.data.notificationChannels[0].verificationStatus='UNVERIFIED';
    await assert.rejects(reconcile(api,'alerts@example.test',true));assert.equal(api.writes.length,0);
  }
});
test('foreign policy with colliding name aborts before creating checks',async()=>{
  const api=fakeAPI();api.data.alertPolicies=[{name:'foreign',displayName:'Selecto - errores HTTP 5xx'}];
  await assert.rejects(reconcile(api,'alerts@example.test',true),/ajeno/);
  assert.equal(api.writes.length,0);
});
test('duplicate ownership aborts without touching resources',async()=>{
  const api=fakeAPI();const c=uptimeConfigs()[0];
  api.data.uptimeCheckConfigs=[{...c,name:'one'},{...c,name:'two'}];
  await assert.rejects(reconcile(api,'alerts@example.test',true),/Duplicados/);
  assert.equal(api.writes.length,0);
});
test('partial failure records successful IDs and does not retry POST',async()=>{
  const api=fakeAPI();const original=api.request;
  api.request=async(...args)=>{if(api.writes.length===2)throw new Error('network failed');return original(...args);};
  await assert.rejects(reconcile(api,'alerts@example.test',true),e=>e.changes.length===2);
  assert.equal(api.writes.length,2);
});
test('server metadata does not trigger drift; meaningful change does',()=>{
  assert.ok(containsDesired({name:'id',enabled:true},{enabled:true}));
  assert.ok(!containsDesired({period:'60s'},{period:'300s'}));
  assert.ok(containsDesired({comparison:'COMPARISON_GT'},{comparison:'COMPARISON_GT',thresholdValue:0}));
  assert.ok(!containsDesired({},{enabled:true}));
});
test('job absence aligns DELTA metrics and also detects samples with zero successes',()=>{
  const p=alertPolicies([],'approved').find(x=>x.userLabels.selecto_key==='jobs-missing');
  assert.equal(p.conditions.length,6);
  for(const c of p.conditions){
    const condition=c.conditionAbsent ?? c.conditionThreshold;
    assert.equal(condition.aggregations[0].perSeriesAligner,'ALIGN_SUM');
  }
  assert.equal(p.conditions.filter(c=>c.conditionAbsent).length,3);
});
