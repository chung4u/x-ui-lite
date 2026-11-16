const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');
const html = fs.readFileSync(path.join(__dirname,'../web/html/xui/index.html'),'utf8');
const classes = html.slice(html.indexOf('    const State ='),html.indexOf('    const versionModal ='));
const body = html.match(/healthIssues\(\) \{([\s\S]*?)\n            \},\n            healthIsHealthy/)[1];
const sandbox = {toFixed:(value,places)=>Number(Number(value).toFixed(places))};
vm.createContext(sandbox);
vm.runInContext(classes+'\nthis.StatusModel = Status; this.issues = function(){'+body+'};',sandbox);
function status(cpuHealth) {
  const data = {cpu:82,mem:{current:20,total:100},disk:{current:20,total:100},swap:{current:0,total:0},loads:[0,0,0],xray:{state:'running'},netIO:{up:0,down:0},netTraffic:{sent:0,recv:0},cpuHealth};
  return new sandbox.StatusModel(data);
}
function issues(view) {
  return sandbox.issues.call({status:view,hasStatusLoaded:true,inboundHealthError:false,inboundHealth:{expired:0,exhausted:0,expiring:0,highUsage:0,disabled:0}});
}
const short = status({sampleValid:true,sustainedHigh:false,highForSeconds:179});
assert.equal(short.cpu.percent,82,'real-time CPU remains visible');
assert.equal(issues(short).some(row=>row.key==='cpu'),false,'179-second CPU spike must not create an issue');
const confirmed = status({sampleValid:true,sustainedHigh:true,highForSeconds:180});
const warning = issues(confirmed).find(row=>row.key==='cpu');
assert.equal(warning.level,'warning'); assert.match(warning.description,/3 分钟/);
confirmed.cpu.current = 90;
assert.equal(issues(confirmed).find(row=>row.key==='cpu').level,'danger','existing 90% severity remains');
confirmed.cpu.current = 79;
assert.equal(issues(confirmed).some(row=>row.key==='cpu'),false,'live low CPU must not retain a stale issue');
for (const health of [undefined,{sampleValid:false,sustainedHigh:true},{sampleValid:true,sustainedHigh:false},{sampleValid:'true',sustainedHigh:'true'}]) {
  const view = status(health); view.cpu.current = 100;
  assert.equal(issues(view).some(row=>row.key==='cpu'),false,'missing/unconfirmed samples must not create CPU issues');
}
const other = status(undefined);
other.mem.current = 80; other.disk.current = 90; other.xray.state = 'stop';
assert.equal(issues(other).find(row=>row.key==='mem').level,'warning');
assert.equal(issues(other).find(row=>row.key==='disk').level,'danger');
assert.equal(issues(other).find(row=>row.key==='xray').level,'danger');
assert.equal(issues(status({sampleValid:true,sustainedHigh:true,highForSeconds:180})).some(row=>row.key==='cpu'),true,'a fresh page uses the server confirmation without restarting a browser timer');
console.log('PASS: CPU issue requires server confirmation, real-time CPU stays visible, missing/stale samples suppress CPU issues and existing memory/disk/core rules remain');
