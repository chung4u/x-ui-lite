const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const assert = require('node:assert/strict');
const root = path.join(__dirname, '..');
const messages = [];
const sandbox = {
  console, URL, location: {hostname: 'panel.example.test'},
  moment(value) { return {valueOf: () => value, format: () => String(value)}; },
  app: {$message: {error: s => messages.push(s), warning: s => messages.push(s), success() {}}},
  Vue: function Vue() {},
};
sandbox.Vue.set = (o, k, v) => { o[k] = v; };
sandbox.Vue.delete = (o, k) => { delete o[k]; };
sandbox.Vue.prototype.$confirm = options => options.onOk();
vm.createContext(sandbox);
for (const file of ['web/assets/js/util/common.js', 'web/assets/js/util/utils.js',
  'web/assets/js/model/xray.js', 'web/assets/js/model/models.js']) {
  vm.runInContext(fs.readFileSync(path.join(root, file), 'utf8'), sandbox);
}
const html = fs.readFileSync(path.join(root, 'web/html/xui/inbound_users_modal.html'), 'utf8');
vm.runInContext(html.match(/<script>([\s\S]*?)<\/script>/)[1] + '\nthis.ui = userModal;', sandbox);
const detailHTML = fs.readFileSync(path.join(root, 'web/html/xui/inbound_info_modal.html'), 'utf8');
vm.runInContext(detailHTML.match(/<script>([\s\S]*?)<\/script>/)[1] + '\nthis.detail = infoModal;', sandbox);
vm.runInContext(`
this.fixture = new DBInbound({id: 42, port: 18081, listen: '172.18.0.1', protocol: 'vless',
 externalHost: 'proxy.example.test', externalPort: 443, externalTls: true,
 settings: JSON.stringify({clients: [{id:'11111111-1111-4111-8111-111111111111',email:'A',flow:''},
 {id:'22222222-2222-4222-8222-222222222222',email:'B',flow:''},
 {id:'33333333-3333-4333-8333-333333333333',email:'C',flow:''}], decryption:'none', fallbacks:[]}),
 streamSettings: JSON.stringify({network:'xhttp',security:'none',xhttpSettings:{host:'proxy.example.test',path:'/connect/test',mode:'auto',
 extra:{xmux:{hMaxRequestTimes:'600-900'},xPaddingBytes:'100-1000'},scStreamUpServerSecs:'20-80'}}),
 sniffing: '{}'});
this.model = fixture.toInbound();
this.serialized = model.toJson();
`, sandbox);
assert.equal(sandbox.fixture.connectionLabel, 'proxy.example.test:443');
assert.equal(sandbox.fixture.transportLabel, 'XHTTP＋TLS');
assert.equal(sandbox.model.stream.security, 'none', 'external TLS must not enable TLS on the h2c backend');
const expectedSettings = JSON.parse(sandbox.fixture.streamSettings).xhttpSettings;
assert.deepEqual(JSON.parse(JSON.stringify(sandbox.serialized.streamSettings.xhttpSettings)), expectedSettings);
sandbox.serialized.streamSettings.xhttpSettings.extra.xmux.hMaxRequestTimes = 'changed';
assert.equal(sandbox.model.stream.xhttp.extra.xmux.hMaxRequestTimes, '600-900', 'serialization must not alias nested xhttp options');
const link = new URL(sandbox.fixture.genLink());
assert.equal(link.host, 'proxy.example.test:443');
assert.equal(link.searchParams.get('security'), 'tls');
assert.equal(link.searchParams.get('path'), '/connect/test');
assert.equal(link.searchParams.get('mode'), 'stream-up');
const listHTML = fs.readFileSync(path.join(root, 'web/html/xui/inbounds.html'), 'utf8');
const payloadBody = listHTML.match(/inboundPayload\(inbound, dbInbound\) \{([\s\S]*?)\n            \},/)[1];
const buildPayload = new Function('inbound', 'dbInbound', payloadBody);
const payload = buildPayload(sandbox.model, sandbox.fixture);
assert.equal(payload.streamSettings, sandbox.fixture.streamSettings, 'managed transport must be passed through exactly');
assert.equal(payload.externalPort, 443);
assert.equal(payload.externalTls, true);
assert.equal(payload.resetTraffic, false, 'ordinary edits must not reset traffic');

const ids = sandbox.model.settings.vlesses.map(u => u.id);
let view = {
  users: ids.map((clientId, i) => ({clientId, enabled:true, effectiveEnabled:true, groupId:7, up:100+i, down:200})),
  groups:[{id:7,name:'Shared',total:500*1024**3,up:99,down:11,expiryTime:1814284800015,enabled:true}],
};
let calls = [];
sandbox.mockPost = async (url, data) => {
  calls.push({url,data});
  if (url.endsWith('/userPolicies')) return {success:true,obj:JSON.parse(JSON.stringify(view))};
  return {success:true,obj:{id:8}};
};
vm.runInContext('HttpUtil.post = (url, data) => mockPost(url, data);', sandbox);

async function test() {
  const ui = sandbox.ui;
  ui.show({dbInbound:sandbox.fixture,inbound:sandbox.model,confirm() {}});
  await ui.loadPolicies();
  assert.equal(ui.groupMemberCount(7),3);
  assert.match(ui.groupSummary(ids[0]),/3 人共享/);
  assert.equal(ui.canExport(ids[0]),true);
  ui.users[0].email='Unsaved';
  assert.equal(ui.canEditPolicy(ids[0]),false);
  assert.equal(ui.canExport(ids[0]),false);
  ui.users[0].email='A';
  ui.editGroup(ids[0]);
  assert.equal(ui.groupEditor.expiryValue.valueOf(),1814284800015,'opening group editor must preserve original expiry milliseconds');

  const editor = ui.groupEditor;
  calls=[];
  sandbox.mockPost = async (url,data) => {calls.push({url,data});return {success:false};};
  await ui.saveGroup();
  assert.equal(calls.length,1,'failed quota save must not update assignment');
  assert.equal(ui.groupEditor,editor,'failed save must preserve editable draft');
  assert.equal(ui.groupSaving,false);

  ui.groupEditor=null;
  ui.newGroup(ids[0]);
  calls=[];
  sandbox.mockPost = async (url,data) => {
    calls.push({url,data});
    if (url.endsWith('/quotaGroup/update')) return {success:true,obj:{id:8}};
    if (url.endsWith('/userPolicies/update')) return {success:false};
    return {success:true,obj:JSON.parse(JSON.stringify(view))};
  };
  await ui.saveGroup();
  assert.ok(messages.some(s=>s.includes('额度已创建')));
  assert.equal(ui.policyFor(ids[0]).groupId,7,'failed assignment must retain actual previous group');

  sandbox.mockPost = async () => ({success:false});
  await ui.loadPolicies();
  assert.equal(ui.policyError,true);
  assert.equal(ui.canExport(ids[0]),false);
  assert.equal(ui.policyStatus(ids[0]),'状态未知');

  sandbox.mockPost = async () => ({success:true,obj:JSON.parse(JSON.stringify(view))});
  await ui.loadPolicies();
  ui.policies[0].effectiveEnabled=false;ui.policies[0].reason='额度已用尽';
  assert.equal(ui.canExport(ids[0]),false);
  assert.equal(ui.policyStatus(ids[0]),'额度已用尽');

  let rejectOld;
  sandbox.mockPost = () => new Promise((resolve, reject) => {rejectOld=reject;});
  const oldRequest=ui.loadPolicies();
  sandbox.mockPost = async () => ({success:true,obj:JSON.parse(JSON.stringify(view))});
  await ui.loadPolicies();
  rejectOld(new Error('stale request failed'));
  await oldRequest;
  assert.equal(ui.policyError,false,'a stale request must not overwrite current policies');
  assert.equal(ui.hasPolicy(ids[0]),true);

  let closeConfirmation=null;
  sandbox.Vue.prototype.$confirm = options => {closeConfirmation=options;};
  ui.newGroup(ids[0]);
  ui.requestClose();
  assert.ok(closeConfirmation,'closing an unsubmitted quota form must ask before discarding');
  assert.equal(ui.visible,true);
  let savedUsers=false;
  ui.confirm=()=>{savedUsers=true;};
  ui.save();
  assert.equal(savedUsers,false,'saving users must not silently discard a quota draft');
  assert.ok(messages.some(s=>s.includes('请先保存或取消额度编辑')));

  const detail=sandbox.detail;
  calls=[];
  sandbox.mockPost=async (url,data)=>{calls.push({url,data});return {success:true,obj:[]};};
  detail.show({dbInbound:sandbox.fixture});
  assert.equal(calls.some(call=>call.url.endsWith('/endpointStatus')),false,'certificate probing must be manual');
  assert.equal(detail.certificateLabel,'尚未检查');
  for(const [state,label] of [['valid','证书有效'],['invalid','证书校验失败'],['unreachable','无法验证证书']]) {
    sandbox.mockPost=async(url,data)=>{assert.equal(data,undefined,'certificate target must come from the server record');return {success:true,obj:{state,notAfter:1814284800015}};};
    await detail.checkEndpointStatus();
    assert.equal(detail.certificateLabel,label);
    assert.equal(detail.endpointLoading,false);
  }
  sandbox.mockPost=async()=>{throw new Error('network unavailable');};
  await detail.checkEndpointStatus();
  assert.equal(detail.certificateLabel,'无法验证证书','a failed request must not retain a valid certificate badge');
  console.log('PASS: XHTTP round-trip/public endpoint, shared quotas, expiry precision, dirty guards, partial failures, stale reads, manual certificate status and unknown states');
}
test().catch(error => {console.error(error);process.exitCode=1;});
