const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const html = fs.readFileSync(path.join(__dirname, '../web/html/xui/index.html'), 'utf8');
let trend;
function Vue() {}
Vue.component = (name, config) => { if (name === 'runtime-trend') trend = config; };
const calls = [];
const timers = new Map();
let next = 0;
const document = {hidden:false};
const time = Math.floor(Date.now()/1000);
const HttpUtil = {async post(url) {
    calls.push(url);
    if (url.endsWith('Summary')) return {success:true, obj:[]};
    return {success:true, obj:url.endsWith('=0') ? [{time}] : []};
}};
vm.runInNewContext(html.match(/<script>([\s\S]*)<\/script>/)[1], {
    Vue, document, HttpUtil, siderDrawer:{}, moment:require('../web/assets/moment/moment.min.js'),
    setTimeout(fn, delay) { assert.equal(delay,300000); timers.set(++next,fn); return next; },
    clearTimeout(id) { timers.delete(id); }
});
(async () => {
    const view=Object.assign(trend.data(),trend.methods);
    await view.load();
    assert.deepEqual(calls,['/server/runtimeTrendSummary']);
    view.expanded=true;
    await view.load();
    await view.load();
    assert.deepEqual(calls.filter(v=>v.includes('?')),['/server/runtimeTrend?since=0','/server/runtimeTrend?since='+time]);
    const before=calls.length;
    document.hidden=true;
    await view.load();
    assert.equal(timers.size,0);
    assert.equal(calls.length,before);
    document.hidden=false;
    await view.load();
    assert.equal(calls.at(-1),'/server/runtimeTrend?since='+time);
})().catch(err=>{console.error(err);process.exitCode=1;});
