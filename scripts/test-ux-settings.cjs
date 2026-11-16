const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const html = fs.readFileSync(path.join(__dirname, '../web/html/xui/setting.html'), 'utf8');
const models = fs.readFileSync(path.join(__dirname, '../web/assets/js/model/models.js'), 'utf8');
let options, responder, reloads = 0, timerID = 0;
let currentHref = 'https://panel.example.test:8080/old/xui/setting?tab=panel';
const timers = new Map(), events = new Map();
function Vue(config) { options = config; }
const sandbox = {
    Vue, URL, URLSearchParams, console,
    ObjectUtil: {isEmpty: value => value == null || value === '', cloneProps: (target, source) => Object.keys(target).forEach(key => { if (key in source) target[key] = source[key]; })},
    HttpUtil: {post: (...args) => responder(...args)},
    PromiseUtil: {sleep: async () => {}}, siderDrawer: {}, basePath: '/panel/',
    location: {get href() { return currentHref; }, set href(value) { currentHref = new URL(value, currentHref).href; }, hostname: 'panel.example.test', pathname: '/old/xui/setting', search: '?tab=panel', reload: () => reloads++},
    window: {setTimeout: (fn, delay) => {timers.set(++timerID, {fn, delay}); return timerID;}, clearTimeout: id => timers.delete(id),
        addEventListener: (name, fn) => events.set(name, fn), removeEventListener: name => events.delete(name)},
    document: {addEventListener() {}, removeEventListener() {}}, history: {replaceState() {}},
};
vm.createContext(sandbox);
vm.runInContext(models.slice(models.indexOf('class AllSetting {')) + '\nthis.AllSetting = AllSetting;', sandbox);
vm.runInContext(html.match(/<script>([\s\S]*)<\/script>/)[1], sandbox);
function instance() {
    const messages = [];
    const view = {...options.data, oldAllSetting: new sandbox.AllSetting(), allSetting: new sandbox.AllSetting(), user: {},
        applyState: {known: false, pendingRestartKeys: [], trafficPending: false, corePending: false},
        messages, $message: {error: text => messages.push(text), warning: text => messages.push(text), success: text => messages.push(text)}};
    for (const [name, method] of Object.entries(options.methods)) view[name] = method.bind(view);
    for (const [name, compute] of Object.entries(options.computed)) Object.defineProperty(view, name, {get: () => compute.call(view)});
    view.confirmDialog = async () => true;
    return view;
}
function config() {
    return {...new sandbox.AllSetting(), webPort: 8443, webBasePath: '/panel/', xrayTemplateConfig: '{}', trafficLimitGB: 1000};
}
const synced = () => ({success: true, obj: {known: true, pendingRestartKeys: [], trafficPending: false, corePending: false}});
(async () => {
    const initial = instance();
    assert.equal(initial.basePath, '/panel/', 'the core-status template link has an explicit Vue binding');
    assert.match(initial.settingsStateLabel, /正在读取/);
    assert.equal(initial.settingsStateTone, 'neutral');
    responder = async () => {throw new Error('offline');};
    assert.equal(await initial.getAllSetting(), false);
    assert.equal(initial.settingsLoaded, false);
    assert.equal(initial.spinning, false);
    assert.equal(initial.settingsStateLabel, '配置暂不可用');
    responder = async url => url.endsWith('/all') ? {success: true, obj: config()} : synced();
    assert.equal(await initial.getAllSetting(), true);
    assert.equal(initial.settingsLoaded, true);
    assert.equal(initial.settingsStateLabel, '启动配置已同步');
    assert.equal(initial.settingsStateTone, 'saved');
    initial.applyState = {known: true, pendingRestartKeys: ['timeLocation'], trafficPending: true, corePending: false};
    assert.equal(initial.settingsStateLabel, '已保存，待重启生效');
    initial.applyState.corePending = true;
    assert.equal(initial.settingsStateLabel, '核心配置尚未应用');
    assert.equal(initial.settingsStateTone, 'pending', 'an unapplied core must never appear green');
    initial.allSetting.trafficLimitGB = 2000;
    assert.equal(initial.settingsStateLabel, '1 项待保存', 'unsaved changes have priority over pending restart');
    initial.allSetting.trafficLimitGB = 1000;
    initial.applyState.corePending = false;
    initial.applyState = {known: true, pendingRestartKeys: [], trafficPending: false, corePending: true};
    assert.equal(initial.settingsStateLabel, '核心配置尚未应用');
    assert.equal(initial.settingsStateTone, 'pending', 'core-only pending state cannot use the green synced style');
    initial.applyState = {known: true, pendingRestartKeys: [], trafficPending: true, corePending: false};
    assert.equal(initial.settingsStateLabel, '已保存，流量更新中');
    responder = async () => ({success: true, obj: {known: false}});
    await initial.getApplyState();
    assert.equal(initial.settingsStateLabel, '生效状态待确认');
    assert.equal(initial.settingsStateTone, 'neutral');
    responder = async () => ({success: true, obj: {known: true, pendingRestartKeys: ['futureUnknownKey'], trafficPending: false, corePending: false}});
    await initial.getApplyState();
    assert.equal(initial.applyState.known, false, 'unknown pending keys cannot be silently reported as applied');

    const race = instance();
    const resolvers = [];
    responder = () => new Promise(resolve => resolvers.push(resolve));
    const older = race.getApplyState(), newer = race.getApplyState();
    resolvers[1](synced()); await newer;
    resolvers[0]({success: true, obj: {known: true, pendingRestartKeys: ['webPort'], trafficPending: false, corePending: false}}); await older;
    assert.equal(race.applyState.pendingRestartKeys.length, 0, 'late reads cannot replace a newer state');
    responder = async () => ({success: true, obj: {known: true, pendingRestartKeys: [], trafficPending: true, corePending: false}});
    await race.getApplyState();
    assert.equal(timers.get(race.applyStateTimer).delay, 3000, 'pending traffic schedules another state check');
    responder = async () => ({success: true, obj: {known: true, pendingRestartKeys: [], trafficPending: false, corePending: true}});
    await race.getApplyState();
    assert.equal(race.applyState.corePending, true);
    assert.equal(timers.get(race.applyStateTimer).delay, 3000, 'an unapplied core also schedules a read-only check');
    let hiddenRequests = 0;
    responder = async () => {hiddenRequests++; return synced();};
    sandbox.document.hidden = true;
    await race.getApplyState();
    assert.equal(hiddenRequests, 0, 'hidden pages pause apply-state requests');
    sandbox.document.hidden = false;
    await race.getApplyState();
    assert.equal(race.applyState.corePending, false);

    const save = instance(); save.applyAllSetting(config()); save.settingsLoaded = true;
    save.allSetting.timeLocation = 'UTC';
    let stored = config(); const calls = [];
    responder = async (url, data) => {
        calls.push(url);
        if (url.endsWith('/update')) { stored = {...data}; return {success: true}; }
        if (url.endsWith('/all')) return {success: true, obj: stored};
        return {success: true, obj: {known: true, pendingRestartKeys: ['timeLocation'], trafficPending: false, corePending: false}};
    };
    await save.updateAllSetting();
    assert.equal(save.saveBtnDisable, true);
    assert.equal(save.settingsStateLabel, '已保存，待重启生效');
    assert.ok(calls.includes('/xui/setting/applyState'), 'saving rechecks effective state');
    save.allSetting.trafficLimitGB = 2222;
    responder = async () => {throw new Error('offline');};
    await save.updateAllSetting();
    assert.equal(save.savingSettings, false);
    assert.equal(save.allSetting.trafficLimitGB, 2222, 'failed saving retains edits');

    const admin = instance(); admin.applyAllSetting(config()); admin.settingsLoaded = true; admin.applyState = synced().obj;
    admin.user = {oldUsername: 'qa-admin', oldPassword: 'qa-old', newUsername: 'qa-new', newPassword: 'qa-updated'};
    assert.equal(admin.hasAccountDraft, true);
    assert.equal(admin.accountCanSubmit, true);
    assert.equal(admin.saveBtnDisable, true, 'admin edits are independent from the global config button');
    assert.match(admin.accountStateLabel, /未提交/);
    responder = async () => ({success: false}); await admin.updateUser();
    assert.equal(admin.hasAccountDraft, true, 'failed account updates retain the draft');
    responder = async () => ({success: true}); await admin.updateUser();
    assert.equal(admin.hasAccountDraft, false);
    assert.equal(admin.accountUpdated, true);
    assert.equal(admin.updatingUser, false);
    assert.equal(sandbox.location.href, 'https://panel.example.test:8080/panel/', 'credential changes return to login after revoking sessions');
    admin.allSetting.webCertFile = '/qa/cert.pem'; admin.allSetting.webKeyFile = '/qa/key.pem';
    assert.equal(admin.certificateStatus, '证书路径已配置', 'paths do not claim actual public HTTPS validity');
    admin.allSetting.webPort = 9443;
    assert.equal(new URL(admin.panelAddressPreview).port, '8443', 'address preview uses saved values');
    assert.equal(new URL(admin.panelAddressPreview).pathname, '/panel/');
    assert.equal(new URL(admin.panelAddressPreview).search, '');

    const restart = instance(); restart.applyAllSetting(config()); restart.settingsLoaded = true;
    restart.applyState = {known: true, pendingRestartKeys: ['webPort'], trafficPending: false, corePending: false};
    responder = async () => ({success: true});
    await restart.restartPanel();
    assert.equal(restart.restartRequested, true);
    assert.equal(reloads, 0, 'connectivity changes must not redirect or reload an uncertain proxy URL');
    assert.match(restart.settingsStateLabel, /连接待确认/);
    const regular = instance(); regular.applyAllSetting(config()); regular.settingsLoaded = true;
    regular.applyState = {known: true, pendingRestartKeys: ['timeLocation'], trafficPending: false, corePending: false};
    await regular.restartPanel();
    assert.equal(reloads, 1, 'unchanged connectivity preserves the normal restart reload');

    const unload = instance();
    responder = async url => url.endsWith('/all') ? {success: true, obj: config()} : synced();
    await options.mounted.call(unload);
    unload.user.oldUsername = 'qa-draft';
    let prevented = false;
    events.get('beforeunload')({preventDefault() {prevented = true;}});
    assert.equal(prevented, true, 'unsubmitted admin forms have navigation protection');
    options.beforeDestroy.call(unload);
    assert.ok(html.includes('v-if="settingsLoaded" class="settings-card"'));
    assert.ok(html.includes('tab="管理员账号"'));
    assert.ok(html.includes('查看核心状态'));
    console.log('PASS: settings loading and apply-state priority, race protection, save failure retention, independent admin updates, and safe restart/address preview');
})().catch(error => {console.error(error); process.exitCode = 1;});
