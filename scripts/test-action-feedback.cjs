const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

let options, respond, clipboardResult;
let now=0, timerSequence=0;
const timers=new Map(), allTimers=new Map(), copies=[], requests=[], qrCalls=[], downloads=[];
const browser={sessionStorage:{getItem:()=>null,setItem(){}},addEventListener(){},removeEventListener(){}};
const sandbox={
    Vue:{component:(_,value)=>{options=value;}},
    HttpUtil:{post:(...args)=>{requests.push(args);return respond(...args);}},
    userModal:{copyText:(...args)=>{copies.push(args);return clipboardResult(...args);}},
    qrModal:{show:(...args)=>qrCalls.push(args)},app:{refreshRuntimeAfterSave(){}},window:browser,
    Blob:class {constructor(parts,options){this.parts=parts;this.options=options;}},
    URL:{createObjectURL:()=> 'blob:qa-test',revokeObjectURL(){}},
    document:{body:{appendChild(){}},createElement:()=>({click(){downloads.push(this.download);},remove(){}})},
    location:{hostname:'qa.example.test',pathname:'/xui/inbounds'},
    sizeFormat:value=>String(value),moment:value=>({valueOf:()=>value,format:()=>String(value)}),
    crypto:require('node:crypto').webcrypto,ONE_GB:1073741824,
    setTimeout:(callback,delay)=>{const id=++timerSequence, timer={id,callback,due:now+delay};timers.set(id,timer);allTimers.set(id,timer);return id;},
    clearTimeout:id=>timers.delete(id),setInterval:()=>0,clearInterval(){},
};
vm.runInNewContext(fs.readFileSync('web/assets/js/managed-workspace.js','utf8'),sandbox);
function advance(milliseconds) {
    now+=milliseconds;
    for (const timer of [...timers.values()].sort((a,b)=>a.due-b.due)) {
        if(timer.due<=now && timers.has(timer.id)){timers.delete(timer.id);timer.callback();}
    }
}
function entry(id, users=[{id:'a',email:'A'},{id:'b',email:'B'}]) {return {id,enable:true,trafficResetDay:1,settings:JSON.stringify({clients:users})};}
function policyRows() {return ['a','b'].map(clientId=>({clientId,enabled:true,effectiveEnabled:true,total:0,expiryTime:0,up:0,down:0,resetDay:1}));}
function instance() {
    const messages=[];
    const value={...options.data(),entries:[entry(7),entry(8)],listError:false,refresh:async()=>{},
        $refs:{},$nextTick:callback=>callback(),$set:(object,key,next)=>object[key]=next,$delete:(object,key)=>delete object[key],
        $confirm(){},$message:{success:message=>messages.push(['success',message]),error:message=>messages.push(['error',message])},messages};
    for (const [key,method] of Object.entries(options.methods)) value[key]=method.bind(value);
    for (const [key,getter] of Object.entries(options.computed)) Object.defineProperty(value,key,{get:()=>getter.call(value)});
    value.snapshotId=7;value.hasSnapshot=true;value.policies=policyRows();
    return value;
}
function reset() {timers.clear();allTimers.clear();copies.length=0;requests.length=0;qrCalls.length=0;downloads.length=0;now=0;respond=async()=>({success:true,obj:{url:'https://qa.example.test/subscription'}});clipboardResult=async()=>true;}
function deferred() {let resolve;const promise=new Promise(done=>resolve=done);return {promise,resolve};}
async function settle() {await Promise.resolve();await Promise.resolve();}

async function checkActualClipboardHelper() {
    const html=fs.readFileSync('web/html/xui/inbound_users_modal.html','utf8');
    const start=html.indexOf('        async copyText('), end=html.indexOf('\n        async copySubscription(',start);
    assert.ok(start>0 && end>start);
    const method=html.slice(start,end).trim().replace(/,$/,'');
    let successes=[], errors=[], nativeWrites=0, added=0, removed=0, nativeThrows=false, fallback=true;
    const env={navigator:{clipboard:{writeText:async()=>{nativeWrites++;if(nativeThrows)throw new Error('permission');}}},
        app:{$message:{success:message=>successes.push(message),error:message=>errors.push(message)}},
        document:{body:{appendChild(){added++;}},createElement:()=>({style:{},setAttribute(){},select(){},remove(){removed++;}}),execCommand:()=>fallback}};
    const helper=vm.runInNewContext('({'+method+'}).copyText',env);
    assert.equal(await helper('test value','copied'),true);assert.deepEqual(successes,['copied']);assert.equal(added,0);
    successes=[];assert.equal(await helper('test value','copied',false),true);assert.equal(successes.length,0);assert.equal(nativeWrites,2);
    nativeThrows=true;
    assert.equal(await helper('test value','fallback copied'),true);assert.deepEqual(successes,['fallback copied']);assert.equal(added,1);assert.equal(removed,1);
    successes=[];assert.equal(await helper('test value','hidden success',false),true);assert.equal(successes.length,0);assert.equal(added,2);assert.equal(removed,2);
    env.navigator.clipboard=null;
    assert.equal(await helper('test value','legacy copied'),true);assert.deepEqual(successes,['legacy copied']);
    successes=[];fallback=false;
    assert.equal(await helper('test value','no success',false),false);assert.equal(successes.length,0);assert.equal(errors.length,1);
    assert.equal(added,removed,'every fallback textarea must be removed, including failed copying');
}

(async()=>{
    await checkActualClipboardHelper();
    reset();
    const basic=instance(), user=basic.users[0];
    await basic.distribute(user,'subscription');
    assert.equal(basic.subscriptionCopied(user),true);assert.equal(basic.subscriptionCopied(basic.users[1]),false);
    assert.equal(copies.length,1);assert.equal(copies[0][2],false,'only subscription success disables the shared global toast');
    assert.equal(basic.messages.length,0);assert.equal(requests.length,1);assert.equal(basic.pending.a,undefined);
    assert.equal(timers.size,1);
    advance(1499);assert.equal(basic.subscriptionCopied(user),true);
    advance(1);assert.equal(basic.subscriptionCopied(user),false);assert.equal(timers.size,0);assert.equal(requests.length,1,'feedback expiry must not generate another subscription');

    reset();
    const repeat=instance();await repeat.distribute(repeat.users[0],'subscription');
    const oldTimer=[...allTimers.values()][0];advance(1000);
    await repeat.distribute(repeat.users[0],'subscription');
    assert.equal(timers.size,1);oldTimer.callback();assert.equal(repeat.subscriptionCopied(repeat.users[0]),true,'a previously queued canceled callback must not clear newer feedback');
    advance(1499);assert.equal(repeat.subscriptionCopied(repeat.users[0]),true);advance(1);assert.equal(repeat.subscriptionCopied(repeat.users[0]),false);assert.equal(requests.length,2);

    reset();
    const rows=instance();await rows.distribute(rows.users[0],'subscription');advance(500);await rows.distribute(rows.users[1],'subscription');
    advance(1000);assert.equal(rows.subscriptionCopied(rows.users[0]),false);assert.equal(rows.subscriptionCopied(rows.users[1]),true);advance(500);assert.equal(rows.subscriptionCopied(rows.users[1]),false);

    reset();
    const locked=instance(), response=deferred();respond=()=>response.promise;
    const pending=locked.distribute(locked.users[0],'subscription');
    assert.equal(locked.canExport(locked.users[0]),false);
    await locked.distribute(locked.users[0],'subscription');await locked.distribute(locked.users[0],'qr');assert.equal(requests.length,1);
    response.resolve({success:false});await pending;
    assert.equal(locked.pending.a,undefined);assert.match(locked.exportErrors.a,/请重试/);assert.equal(locked.subscriptionCopied(locked.users[0]),false);assert.equal(locked.messages.at(-1)[0],'error');
    respond=async()=>({success:true,obj:{url:'test subscription'}});clipboardResult=async()=>false;
    await locked.distribute(locked.users[0],'subscription');assert.match(locked.exportErrors.a,/复制失败/);assert.equal(timers.size,0);
    clipboardResult=async()=>true;await locked.distribute(locked.users[0],'subscription');assert.equal(locked.exportErrors.a,undefined);assert.equal(locked.subscriptionCopied(locked.users[0]),true);
    clipboardResult=async()=>false;await locked.distribute(locked.users[0],'subscription');assert.equal(locked.subscriptionCopied(locked.users[0]),false);assert.equal(timers.size,0,'a failed repeat cannot retain the previous success indicator');

    reset();
    const link=instance();respond=async()=>({success:true,obj:{link:'vless://test'}});
    await link.distribute(link.users[0],'link');assert.equal(copies[0].length,2);assert.equal(copies[0][1],'节点链接已复制');assert.equal(timers.size,0);
    respond=async()=>({success:true,obj:{link:'vless://test',clashVergeConfig:'proxies: []',fileName:'qa.yaml',connection:{name:'US | RoyLive'}}});
    await link.distribute(link.users[0],'qr');assert.equal(qrCalls.length,1);assert.equal(qrCalls[0][0],'US | RoyLive · 二维码');assert.equal(copies.length,1);
    await link.distribute(link.users[0],'yaml');assert.deepEqual(downloads,['qa.yaml']);assert.equal(link.messages.at(-1)[1],'配置已下载');
    assert.equal(requests.length,3);assert.ok(requests.every(([url])=>url.endsWith('/userProfile')));assert.equal(timers.size,0,'other distribution formats retain their behavior without subscription feedback timers');

    reset();
    const switched=instance(), first=deferred(), second=deferred();
    respond=url=>url.endsWith('/userPolicies')?Promise.resolve({success:true,obj:{users:policyRows()}}):url.includes('/7/')?first.promise:second.promise;
    const oldRequest=switched.distribute(switched.users[0],'subscription');await switched.switchEntry(8);
    const newRequest=switched.distribute(switched.users[0],'subscription');
    first.resolve({success:true,obj:{url:'old subscription'}});await oldRequest;
    assert.equal(switched.pending.a,true,'a late old finally must not unlock the new entry user');assert.equal(copies.length,0);
    second.resolve({success:true,obj:{url:'new subscription'}});await newRequest;
    assert.equal(switched.subscriptionCopied(switched.users[0]),true);assert.equal(copies.length,1);assert.equal(copies[0][0],'new subscription');
    await switched.switchEntry(7);assert.equal(timers.size,0);assert.equal(switched.subscriptionCopied(switched.users[0]),false);

    reset();
    const removed=instance();await removed.distribute(removed.users[0],'subscription');
    let confirmation;removed.$confirm=value=>confirmation=value;removed.removeUser(removed.users[0]);
    respond=async()=>({success:true});removed.refresh=async()=>{removed.entries=[entry(7,[{id:'b',email:'B'}]),entry(8)];};
    await confirmation.onOk();assert.equal(timers.size,0);assert.equal(removed.subscriptionCopied({id:'a'}),false);

    reset();
    const clipboardWait=instance(), clipboard=deferred();clipboardResult=()=>clipboard.promise;
    const awaitingClipboard=clipboardWait.distribute(clipboardWait.users[0],'subscription');await settle();assert.equal(copies.length,1);
    clipboardWait.clearUserAction(clipboardWait.users[0],7);clipboardWait.entries=[entry(7,[{id:'b',email:'B'}])];
    clipboard.resolve(true);await awaitingClipboard;assert.equal(timers.size,0);assert.equal(clipboardWait.pending.a,undefined);assert.equal(clipboardWait.subscriptionCopied({id:'a'}),false);

    reset();
    const late=instance(), lateResponse=deferred();respond=()=>lateResponse.promise;
    const waiting=late.distribute(late.users[0],'subscription');options.beforeDestroy.call(late);
    lateResponse.resolve({success:true,obj:{url:'late subscription'}});await waiting;
    assert.equal(copies.length,0);assert.equal(timers.size,0);assert.equal(Object.keys(late.pending).length,0);
    reset();
    const destroyedClipboard=instance(), stoppedClipboard=deferred();clipboardResult=()=>stoppedClipboard.promise;
    const copying=destroyedClipboard.distribute(destroyedClipboard.users[0],'subscription');await settle();options.beforeDestroy.call(destroyedClipboard);
    stoppedClipboard.resolve(true);await copying;assert.equal(timers.size,0);assert.equal(Object.keys(destroyedClipboard.pending).length,0);
    reset();
    const destroyedTimer=instance();await destroyedTimer.distribute(destroyedTimer.users[0],'subscription');const lateTimer=[...allTimers.values()][0];
    options.beforeDestroy.call(destroyedTimer);assert.equal(timers.size,0);lateTimer.callback();assert.equal(Object.keys(destroyedTimer.subscriptionFeedback).length,0);

    // Exercise Vue 2's real asynchronous watchers, including a removed current-entry fallback.
    reset();
    const Vue=require('../web/assets/vue@2.6.12/vue.runtime.common.js');
    const reactive=new Vue({...options,propsData:{entries:[entry(7),entry(8)],listError:false,refresh:async()=>{}}});
    reactive.snapshotId=7;reactive.hasSnapshot=true;reactive.policies=policyRows();
    const oldReactive=deferred(), newReactive=deferred();
    respond=url=>url.endsWith('/userPolicies')?Promise.resolve({success:true,obj:{users:policyRows()}}):url.includes('/7/')?oldReactive.promise:newReactive.promise;
    const oldReactiveRequest=reactive.distribute(reactive.users[0],'subscription');await reactive.switchEntry(8);
    const newReactiveRequest=reactive.distribute(reactive.users[0],'subscription');await Vue.nextTick();
    oldReactive.resolve({success:true,obj:{url:'reactive old'}});await oldReactiveRequest;assert.equal(reactive.pending.a,true);
    newReactive.resolve({success:true,obj:{url:'reactive new'}});await newReactiveRequest;assert.equal(reactive.subscriptionCopied(reactive.users[0]),true);
    reactive.entries=[entry(7)];await Vue.nextTick();assert.equal(timers.size,0);assert.equal(Object.keys(reactive.subscriptionFeedback).length,0);reactive.$destroy();

    console.log('PASS: native/fallback clipboard toast compatibility and failures, 1500ms row feedback and renewal, isolated rows and entries, pending/retry locking, preserved node behavior, queued timers, deleted users, destruction, late API/clipboard replies, and real Vue watcher cleanup');
})().catch(error=>{console.error(error);process.exit(1);});
