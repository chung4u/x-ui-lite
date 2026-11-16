const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

let options, respond;
let overlays = [];
const intervals = new Map(), cleared = [];
const document = {visibilityState:'visible',querySelectorAll:()=>overlays};
const sandbox = {
    Vue:{component:(_,value)=>{options=value;}},
    HttpUtil:{post:async (url,data)=>{
        if (url.endsWith('/hysteria') && data.action==='status') return {success:true,obj:null};
        return respond(url,data);
    }},
    sizeFormat:value=>String(value),moment:value=>({valueOf:()=>value,format:()=>String(value)}),
    ONE_GB:1073741824,Date,JSON,Number,String,Math,Set,Array,Uint8Array,
    crypto:require('node:crypto').webcrypto,document,
    getComputedStyle:node=>({visibility:node.visibility || 'visible'}),
    setInterval:(callback,delay)=>{const id=intervals.size+1;intervals.set(id,{callback,delay});return id;},
    clearInterval:id=>cleared.push(id),
    location:{hostname:'qa.example.test',pathname:'/xui/inbounds'},
    window:{sessionStorage:{getItem:()=>null,setItem(){}},addEventListener(){},removeEventListener(){}},
    app:{refreshRuntimeAfterSave(){}},userModal:{copyText(){}},qrModal:{show(){}},
};
vm.runInNewContext(fs.readFileSync('web/assets/js/managed-workspace.js','utf8'),sandbox);
function instance() {
    const emitted=[];
    const value={...options.data(),entries:[{id:7,enable:true,trafficResetDay:15,settings:JSON.stringify({clients:[{id:'a',email:'A'},{id:'b',email:'B'}]})}],
        runtimeState:null,listError:false,refresh:async()=>{},$refs:{},$nextTick:callback=>callback(),
        $emit:(...args)=>emitted.push(args),emitted,$message:{success(){},warning(){},error(){}},
        $set:(object,key,next)=>object[key]=next,$delete:(object,key)=>delete object[key]};
    for (const [key,method] of Object.entries(options.methods)) value[key]=method.bind(value);
    for (const [key,getter] of Object.entries(options.computed)) Object.defineProperty(value,key,{get:()=>getter.call(value)});
    value.snapshotId=7;value.hasSnapshot=true;value.checkedAt=123;
    value.policies=['a','b'].map(clientId=>({clientId,enabled:true,effectiveEnabled:true,total:100,expiryTime:0,resetDay:15,up:3,down:4,lastActiveAt:0}));
    return value;
}
const response = up => ({success:true,obj:{users:['a','b'].map(clientId=>({clientId,enabled:true,effectiveEnabled:true,total:100,expiryTime:0,resetDay:15,up,down:4,lastActiveAt:0}))}});

(async()=>{
    const search=instance();
    search.query='B'; search.filter='disabled';
    assert.equal(search.visibleUsers.length,0);
    assert.match(search.emptyDescription,/当前筛选中/);
    search.setFilter('all');
    assert.equal(search.query,''); assert.equal(search.visibleUsers.length,2);
    search.setFilter('attention'); search.query='B';
    const escape={preventDefault(){this.prevented=true;},stopPropagation(){this.stopped=true;}};
    search.clearSearch({...escape,isComposing:true}); assert.equal(search.query,'B');
    search.clearSearch(escape); assert.equal(search.query,''); assert.equal(search.filter,'attention');
    assert.ok(escape.prevented && escape.stopped);
    assert.equal(search.filterStatusNote,''); assert.match(search.emptyDescription,/没有需要关注/);
    search.policies.pop(); assert.equal(search.attentionCount,null);
    assert.match(search.filterStatusNote,/部分用户状态待确认/); assert.match(search.emptyDescription,/无法确认/);
    search.failed=true; assert.match(search.filterStatusNote,/上次读取/);
    search.hasSnapshot=false; assert.match(search.filterStatusNote,/读取失败/);
    search.failed=false; search.loading=true; assert.match(search.filterStatusNote,/正在读取/);
    search.filter='all'; assert.equal(search.filterStatusNote,'');
    search.listError=true; assert.match(search.filterStatusNote,/上次读取的用户列表/);

    const reset=instance();
    assert.equal(reset.sharedResetDay,15);
    reset.policies[0].resetDay=7;assert.equal(reset.sharedResetDay,null);
    reset.openUser(reset.users[0]);assert.equal(reset.editorResetDay,7);
    reset.policies[0].resetDay=40;assert.equal(reset.resetDay(reset.policies[0]),15);
    reset.policies.pop();assert.equal(reset.sharedResetDay,null,'incomplete policies must not claim one common reset day');

    const runtime=instance();assert.equal(runtime.runtimeIssue,'');
    runtime.runtimeState={running:true,applied:true};assert.equal(runtime.runtimeIssue,'');
    runtime.runtimeState={running:true,applied:false};assert.equal(runtime.runtimeIssue,'','ordinary application progress adds no status copy');
    runtime.runtimeState={running:false,applied:false};assert.match(runtime.runtimeIssue,/核心未运行/);
    runtime.runtimeState={running:true,error:'invalid configuration'};assert.match(runtime.runtimeIssue,/上次配置/);

    const silent=instance(), snapshot=silent.policies;
    let finish;
    respond=()=>new Promise(resolve=>finish=resolve);
    const reading=silent.load({silent:true});
    assert.equal(silent.loading,false);assert.equal(silent.silentLoading,true);assert.equal(silent.ready,true);
    assert.equal(silent.checkedAt,123);assert.equal(silent.policies,snapshot);
    assert.equal(silent.canRefreshSilently(),false,'a pending silent request must not overlap another');
    finish(response(10));await reading;
    assert.equal(silent.silentLoading,false);assert.equal(silent.ready,true);assert.equal(silent.policies[0].up,10);
    assert.ok(silent.checkedAt>123);
    assert.deepEqual(silent.emitted.map(event=>event[0]),['refresh-runtime','updated']);
    const successTime=silent.checkedAt, successSnapshot=silent.policies;
    respond=async()=>{throw new Error('temporary disconnection');};
    await silent.load({silent:true});
    assert.equal(silent.policies,successSnapshot);assert.equal(silent.checkedAt,successTime);
    assert.equal(silent.failed,true);assert.equal(silent.ready,false);assert.equal(silent.loading,false);
    assert.equal(silent.emitted.at(-1)[1].failed,true);
    respond=async()=>response(11);await silent.load({silent:true});assert.equal(silent.failed,false);assert.equal(silent.ready,true);

    const gate=instance();
    assert.equal(gate.canRefreshSilently(),true);
    for (const key of ['loading','silentLoading','saving','disposed']) {
        gate[key]=true;assert.equal(gate.canRefreshSilently(),false,key);gate[key]=false;
    }
    gate.editor={};assert.equal(gate.canRefreshSilently(),false);gate.editor=null;
    gate.delivery={};assert.equal(gate.canRefreshSilently(),false);gate.delivery=null;
    gate.distributionOpenId='a';assert.equal(gate.canRefreshSilently(),false);gate.distributionOpenId=null;
    gate.pending.a=true;assert.equal(gate.canRefreshSilently(),false);gate.pending={};
    document.visibilityState='hidden';assert.equal(gate.canRefreshSilently(),false);document.visibilityState='visible';
    overlays=[{getClientRects:()=>[{}]}];assert.equal(gate.canRefreshSilently(),false);
    overlays=[{getClientRects:()=>[{}],visibility:'hidden'}];assert.equal(gate.canRefreshSilently(),true);
    overlays=[{getClientRects:()=>[]}];assert.equal(gate.canRefreshSilently(),true);overlays=[];
    gate.hasSnapshot=false;assert.equal(gate.canRefreshSilently(),false);gate.hasSnapshot=true;
    let calls=0;respond=async()=>{calls++;return response(12);};
    document.visibilityState='hidden';await gate.refreshSilently();assert.equal(calls,0);
    document.visibilityState='visible';await gate.refreshSilently();assert.equal(calls,1);

    const race=instance();let oldReply;
    respond=()=>new Promise(resolve=>oldReply=resolve);
    const oldRead=race.load({silent:true});
    race.entries=[{id:8,enable:true,settings:'{"clients":[]}'}];
    respond=async()=>({success:true,obj:{users:[]}});await race.load();
    oldReply(response(99));await oldRead;
    assert.equal(race.snapshotId,8);assert.equal(race.policies.length,0);

    const lifecycle=instance();respond=async()=>response(15);
    options.mounted.call(lifecycle);await Promise.resolve();await Promise.resolve();
    assert.deepEqual([...intervals.values()].map(item=>item.delay),[30000,45000]);
    options.beforeDestroy.call(lifecycle);
    assert.deepEqual(cleared,[lifecycle.timer,lifecycle.refreshTimer]);assert.equal(lifecycle.disposed,true);
    const destroyed=instance();respond=()=>new Promise(resolve=>finish=resolve);
    const late=destroyed.load({silent:true});options.beforeDestroy.call(destroyed);finish(response(50));await late;
    assert.equal(destroyed.checkedAt,123);assert.equal(destroyed.policies[0].up,3);assert.equal(destroyed.emitted.length,0);

    console.log('PASS: shared and individual reset dates, exception-only runtime notices, quiet refresh without spinner or export blocking, stale snapshot retention and retry, visibility/interaction guards, request races, timer cleanup and late replies');
})().catch(error=>{console.error(error);process.exit(1);});
