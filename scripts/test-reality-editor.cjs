const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');
const {webcrypto} = require('node:crypto');
const html = fs.readFileSync(path.join(__dirname, '../web/html/xui/inbound_modal.html'), 'utf8');
const script = html.match(/<script>([\s\S]*?)<\/script>/)[1];
let vueOptions;
function Vue(options) { vueOptions = options; }
Vue.set = (object, key, value) => { object[key] = value; };
const sandbox = { Vue, console, window: {crypto: webcrypto, setTimeout() {}}, moment: value => ({valueOf: () => value}) };
vm.createContext(sandbox);
for (const file of ['web/assets/js/util/common.js', 'web/assets/js/util/utils.js', 'web/assets/js/model/xray.js', 'web/assets/js/model/models.js']) {
  vm.runInContext(fs.readFileSync(path.join(__dirname, '..', file), 'utf8'), sandbox);
}
vm.runInContext('HttpUtil.post = (...args) => mockPost(...args); this.InboundModel = Inbound;', sandbox);
vm.runInContext(script + '\nthis.modal = inModal; this.sites = realitySiteOptions;', sandbox);
const modal = sandbox.modal;
const make = (ids, host = 'www.speedtest.org') => {
  modal.inbound = {port: 15000, protocol:'vless', settings: {vlesses: [{flow: ''}]}, stream: {security:'reality', reality: {shortIds: ids, serverNames:[host], dest:host + ':443', privateKey:'unchanged-key'}}};
  modal.dbInbound = {totalGB: 500}; modal.usedPorts=[];
  modal.realityShortIDGenerationFailed = false;
  modal.visible = true; modal.realityPreset = false; modal.baseline = '';
};
for (const [input, expected] of [[[''], ['']], [[], ['']], [['', 'abcd', '1234'], ['', 'abcd', '1234']], [[' aa ', 'bb'], ['aa', 'bb']]]) {
  make(input); let saved;
  modal.confirm = inbound => { saved = JSON.parse(JSON.stringify(inbound.stream.reality)); };
  modal.ok();
  assert.deepEqual(saved.shortIds, expected);
  assert.deepEqual(saved.serverNames, ['www.speedtest.org']);
  assert.equal(saved.dest, 'www.speedtest.org:443');
  assert.equal(saved.privateKey, 'unchanged-key');
}
for (const ids of [['abc'], ['aa', 'z0'], [null], ['0123456789abcdef00']]) {
  make(ids); let saved=false; modal.confirm = () => {saved=true;}; modal.ok();
  assert.equal(saved,false); assert.ok(modal.validationError);
}

const selectedSite = reality => vueOptions.computed.selectedRealitySite.get.call({realitySettings: reality});
assert.ok(!sandbox.sites.some(site => /apple|icloud/i.test(site.serverName)));
const mozilla = sandbox.sites.find(site => site.serverName === 'addons.mozilla.org');
assert.ok(mozilla);

make(['', 'abcd'], 'swdist.apple.com');
const existing = JSON.stringify(modal.inbound.stream.reality);
modal.ensureRealitySettings();
assert.equal(selectedSite(modal.inbound.stream.reality), '__existing__');
// ensureRealitySettings may fill the export fingerprint, but must preserve connection settings.
const preserved = {...modal.inbound.stream.reality};
delete preserved.clientFingerprint;
assert.equal(JSON.stringify(preserved), existing);

make(['']);
modal.inbound.stream.reality = null;
const fresh = modal.ensureRealitySettings();
assert.equal(fresh.dest, '');
assert.deepEqual(Array.from(fresh.serverNames), ['']);
assert.equal(selectedSite(fresh), undefined);
assert.match(fresh.shortIds[0], /^[0-9a-f]{16}$/);
assert.ok(modal.validate(), 'new REALITY must not save without an explicit target');
const generatedID = fresh.shortIds[0];
modal.ensureRealitySettings();
assert.equal(fresh.shortIds[0], generatedID, 'reading defaults must not rotate the ID');
modal.applyRealitySite(mozilla);
assert.equal(selectedSite(fresh), 'mozilla');
assert.equal(fresh.dest, 'addons.mozilla.org:443');
assert.equal(modal.inbound.settings.vlesses[0].flow, '', 'selecting REALITY must not force Vision');
fresh.privateKey = 'new-key';
assert.equal(modal.validate(), '');

async function checkSecureRandomFallback() {
  delete sandbox.window.crypto;
  make(['']);
  modal.inbound.stream.reality = null;
  const unavailable = modal.ensureRealitySettings();
  assert.equal(unavailable.shortIds[0], '');
  modal.applyRealitySite(mozilla);
  unavailable.privateKey = 'new-key';
  let saved = false;
  modal.confirm = () => { saved = true; };
  modal.ok();
  assert.equal(saved, false, 'missing secure randomness must require explicit generation');
  assert.match(modal.validationError, /随机生成/);
  sandbox.mockPost = async route => {
    assert.equal(route, '/xui/inbound/realityShortID');
    return {success: true, obj: {shortId: '0123456789abcdef'}};
  };
  await vueOptions.methods.generateRealityShortID.call({
    inModal: modal,
    ensureRealitySettings: () => modal.ensureRealitySettings(),
    $message: {success() {}},
  });
  modal.ok();
  assert.equal(saved, true, 'server-side generation must recover unsupported browsers');
  sandbox.window.crypto = webcrypto;
}

const tick = () => new Promise(resolve => setImmediate(resolve));
async function checkNewPresetAndRaces() {
  modal.close();
  let keyCalls = 0, candidateCalls = 0;
  sandbox.mockPost = async (route, payload) => {
    if (route.endsWith('/realityKey')) { keyCalls++; return {success:true,obj:{privateKey:'new-private',publicKey:'new-public'}}; }
    if (route.endsWith('/realityCandidate')) { candidateCalls++; return {success:true,obj:{candidate:payload.candidate,state:'valid',message:'verified'}}; }
    throw Error('unexpected request ' + route);
  };
  modal.show({usedPorts:Array.from({length:10}, (_,i)=>10000+i)});
  assert.equal(modal.realityPreset, true);
  assert.equal(modal.inbound.protocol, 'vless');
  assert.equal(modal.inbound.stream.network, 'tcp');
  assert.equal(modal.inbound.stream.security, 'reality');
  assert.equal(modal.inbound.settings.decryption, 'none');
  assert.equal(modal.inbound.settings.vlesses[0].flow, 'xtls-rprx-vision');
  assert.equal(modal.inbound.stream.reality.clientFingerprint, 'chrome');
  assert.equal(modal.inbound.stream.reality.xver, 0);
  assert.equal(modal.inbound.stream.reality.show, false);
  assert.equal(selectedSite(modal.inbound.stream.reality), 'mozilla');
  assert.match(modal.inbound.stream.reality.shortIds[0], /^[0-9a-f]{16}$/);
  assert.equal(modal.realityKeyLoading, true);
  assert.ok(modal.validate(), 'pending generated configuration cannot save');
  await modal.requestRealityKey();
  await tick();
  assert.equal(keyCalls,1,'repeated initialization must not generate another key');
  assert.equal(candidateCalls,1);
  assert.equal(modal.inbound.stream.reality.privateKey,'new-private');
  assert.equal(modal.realityTargetState,'valid');
  assert.equal(modal.validate(),'');
  assert.equal(modal.isDirty(), false, 'generated defaults alone must not create unsaved edits');

  // Existing custom REALITY settings must not acquire the new preset or rotate.
  const existing = modal.inbound.toJson();
  existing.streamSettings.realitySettings.dest = 'custom.example.com:8443';
  existing.streamSettings.realitySettings.serverNames = ['custom.example.com'];
  existing.settings.clients[0].flow = '';
  const model = sandbox.InboundModel.fromJson(existing);
  modal.show({inbound:model, editingId:12});
  await tick();
  assert.equal(modal.realityPreset,false);
  assert.equal(modal.inbound.stream.reality.dest,'custom.example.com:8443');
  assert.equal(modal.inbound.stream.reality.privateKey,'new-private');
  assert.equal(modal.inbound.settings.vlesses[0].flow,'');
  assert.equal(keyCalls,1);

  // A late key/target response from a closed creation form cannot touch editing.
  let resolveKey, resolveTarget;
  sandbox.mockPost = route => new Promise(resolve => {
    if (route.endsWith('/realityKey')) resolveKey = resolve;
    else if (route.endsWith('/realityCandidate')) resolveTarget = resolve;
  });
  modal.close(); modal.show({});
  modal.close(); modal.show({inbound:model, editingId:12});
  resolveKey({success:true,obj:{privateKey:'stale-private',publicKey:'stale-public'}});
  resolveTarget({success:true,obj:{candidate:'mozilla',state:'valid',message:'stale result'}});
  await tick();
  assert.equal(modal.inbound.stream.reality.privateKey,'new-private');
  assert.equal(modal.realityPublicKey,'');
  assert.equal(modal.realityTargetState,'unchecked');

  // Failed initialization keeps saving blocked and can recover without resets.
  sandbox.mockPost = async route => route.endsWith('/realityKey') ? {success:false} : {success:true,obj:{candidate:'mozilla',state:'unavailable',message:'TLS unavailable'}};
  modal.close(); modal.show({}); await tick();
  assert.equal(modal.inbound.stream.reality.privateKey,'');
  assert.ok(modal.realityKeyError);
  assert.ok(modal.validate());
  sandbox.mockPost = async route => route.endsWith('/realityKey') ? {success:true,obj:{privateKey:'retry-private',publicKey:'retry-public'}} : {success:true,obj:{candidate:'mozilla',state:'valid',message:'verified'}};
  await modal.requestRealityKey(); await modal.checkRealityTarget();
  assert.equal(modal.inbound.stream.reality.privateKey,'retry-private');
  assert.equal(modal.validate(),'');

  // The preset must choose a different random-range port when its initial one is occupied.
  const NativeRandom = vm.runInContext('RandomUtil.randomIntRange', sandbox);
  vm.runInContext('RandomUtil.randomIntRange = () => 15000;', sandbox);
  modal.close(); modal.show({usedPorts:[15000,15001]}); await tick();
  assert.equal(modal.inbound.port,15002);
  sandbox.restorePortRandom = NativeRandom;
  vm.runInContext('RandomUtil.randomIntRange = restorePortRandom;', sandbox);
  modal.close();
  console.log('PASS: REALITY preset, single secure initialization, verified candidate, retained existing settings, stale request isolation, retries and occupied-port fallback');
}

async function checkSecureUUIDCreationAndFallback() {
  const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  const ids = new Set(Array.from({length:100}, () => modal.createSecureUUID()));
  assert.equal(ids.size,100); assert.ok([...ids].every(id=>uuidPattern.test(id)));
  sandbox.window.crypto = {getRandomValues:webcrypto.getRandomValues.bind(webcrypto)};
  assert.match(modal.createSecureUUID(),uuidPattern,'getRandomValues must support browsers without randomUUID');
  sandbox.window.crypto = webcrypto;
  const fixture = new sandbox.InboundModel();
  fixture.settings.vlesses[0].id = 'existing-custom-id';
  fixture.settings.vlesses[0].flow = '';
  modal.show({inbound:fixture,editingId:9});
  await modal.addVlessUser();
  assert.equal(modal.inbound.settings.vlesses[0].id,'existing-custom-id');
  assert.match(modal.inbound.settings.vlesses[1].id,uuidPattern);
  assert.equal(modal.inbound.settings.vlesses[1].flow,'');

  delete sandbox.window.crypto;
  let resolveUUID, uuidCalls = 0;
  sandbox.mockPost = (route,payload) => {
    if (route.endsWith('/realityUUID')) { uuidCalls++; return new Promise(resolve=>{resolveUUID=resolve;}); }
    if (route.endsWith('/realityKey')) return Promise.resolve({success:true,obj:{privateKey:'secure-key',publicKey:'public-key'}});
    if (route.endsWith('/realityShortID')) return Promise.resolve({success:true,obj:{shortId:'0123456789abcdef'}});
    if (route.endsWith('/realityCandidate')) return Promise.resolve({success:true,obj:{candidate:payload.candidate,state:'valid'}});
  };
  modal.close(); modal.show({});
  assert.equal(modal.inbound.settings.vlesses[0].id,'','legacy model UUID must be removed before display');
  assert.equal(modal.isPreparing(),true);
  assert.ok(modal.validate());
  await modal.prepareFirstUserUUID(); assert.equal(uuidCalls,1,'duplicate requests must not create multiple credentials');
  resolveUUID({success:true,obj:{clientId:'11111111-1111-4111-8111-111111111111'}}); await tick();
  assert.equal(modal.inbound.settings.vlesses[0].id,'11111111-1111-4111-8111-111111111111');
  assert.equal(modal.isPreparing(),false); assert.equal(modal.isDirty(),false);

  modal.close(); modal.show({});
  resolveUUID({success:false}); await tick();
  assert.ok(modal.uuidError); assert.equal(modal.isPreparing(),true);
  const retry = modal.prepareFirstUserUUID();
  resolveUUID({success:true,obj:{clientId:'22222222-2222-4222-8222-222222222222'}}); await retry;
  assert.equal(modal.inbound.settings.vlesses[0].id,'22222222-2222-4222-8222-222222222222');

  modal.close(); modal.show({});
  modal.close(); modal.show({inbound:fixture,editingId:9});
  resolveUUID({success:true,obj:{clientId:'33333333-3333-4333-8333-333333333333'}}); await tick();
  assert.equal(modal.inbound.settings.vlesses[0].id,'existing-custom-id','stale UUID result cannot overwrite editing');
  const adding = modal.addVlessUser(); const beforeCalls = uuidCalls;
  await modal.addVlessUser(); assert.equal(uuidCalls,beforeCalls);
  modal.close();
  resolveUUID({success:true,obj:{clientId:'44444444-4444-4444-8444-444444444444'}}); await adding;
  assert.equal(modal.inbound.settings.vlesses.length,1,'closing cancels pending additions');
  sandbox.window.crypto = webcrypto;
  console.log('PASS: secure initial and added UUIDs, server randomness fallback, preserved existing credentials, failure/retry and stale response isolation');
}

async function checkLegacyUserModalSecureAdditions() {
  const userHTML = fs.readFileSync(path.join(__dirname,'../web/html/xui/inbound_users_modal.html'),'utf8');
  sandbox.app = {$message:{error(){},success(){},warning(){}}};
  Vue.delete = (object,key) => { delete object[key]; };
  vm.runInContext(userHTML.match(/<script>([\s\S]*?)<\/script>/)[1]+'\nthis.legacyUsers = userModal;',sandbox);
  const users = sandbox.legacyUsers, fixture = new sandbox.InboundModel();
  fixture.settings.vlesses[0].id = 'existing-id';
  fixture.settings.vlesses[0].flow = '';
  users.show({dbInbound:{},inbound:fixture,confirm(){}});
  await users.addUser();
  assert.equal(users.users[0].id,'existing-id');
  assert.match(users.users[1].id,/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  assert.equal(users.users[1].flow,'');
  delete sandbox.window.crypto;
  let resolveID, calls = 0;
  sandbox.mockPost = () => { calls++; return new Promise(resolve=>{resolveID=resolve;}); };
  const adding = users.addUser(); await users.addUser();
  assert.equal(calls,1);
  users.close(); users.show({dbInbound:{},inbound:fixture,confirm(){}});
  resolveID({success:true,obj:{clientId:'55555555-5555-4555-8555-555555555555'}}); await adding;
  assert.equal(users.users.length,1,'stale server UUID cannot add to a reopened modal');
  const failed = users.addUser(); resolveID({success:false}); await failed;
  assert.ok(users.newUserError); assert.equal(users.users.length,1);
  const retry = users.addUser();
  resolveID({success:true,obj:{clientId:'66666666-6666-4666-8666-666666666666'}}); await retry;
  assert.equal(users.users.length,2); assert.equal(users.users[0].id,'existing-id');
  sandbox.window.crypto = webcrypto;
  console.log('PASS: legacy user modal uses secure credentials, locks duplicate additions and isolates retries/reopened sessions');
}

(async()=>{await checkSecureRandomFallback();await checkNewPresetAndRaces();await checkSecureUUIDCreationAndFallback();await checkLegacyUserModalSecureAdditions();})().catch(error => { console.error(error); process.exitCode = 1; });
