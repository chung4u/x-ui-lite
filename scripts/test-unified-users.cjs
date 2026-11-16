const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const VueRuntime = require('../web/assets/vue@2.6.12/vue.runtime.common.js');
let workspaceOptions, pageOptions, respond;
const storage = new Map(), reads = [];
const location = {hostname:'qa.example.test',pathname:'/xui/inbounds',search:'',hash:''};
const history = {replaceState(_state,_title,url) { const next=new URL(url,'https://qa.example.test');location.search=next.search;location.hash=next.hash; }};
function Vue(options) { pageOptions=options; }
Vue.component=(_name,options)=>workspaceOptions=options;
const sandbox = {Vue,URLSearchParams,URL,location,history,console,Date,JSON,Number,String,Math,Set,Array,
    window:{sessionStorage:{getItem:key=>storage.get(key)||null,setItem:(key,value)=>storage.set(key,value)},scrollTo(){},addEventListener(){},removeEventListener(){},crypto:require('node:crypto').webcrypto},
    moment:value=>({valueOf:()=>value,format:()=>String(value)}),siderDrawer:{},
    inModal:{},userModal:{show(){throw new Error('VLESS must use the unified workspace');}},
    setInterval:()=>0,clearInterval(){},setTimeout,clearTimeout};
vm.createContext(sandbox);
for (const file of ['web/assets/js/util/common.js','web/assets/js/util/utils.js','web/assets/js/model/xray.js','web/assets/js/model/models.js']) vm.runInContext(fs.readFileSync(file,'utf8'),sandbox);
vm.runInContext('HttpUtil.post=(...args)=>mockPost(...args);this.Entry=DBInbound;',Object.assign(sandbox,{mockPost:(...args)=>respond(...args)}));
vm.runInContext(fs.readFileSync('web/assets/js/managed-workspace.js','utf8'),sandbox);
const pageHTML=fs.readFileSync('web/html/xui/inbounds.html','utf8');
vm.runInContext(pageHTML.match(/<script>([\s\S]*?)<\/script>/)[1],sandbox);
const entry=(id,extra={})=>new sandbox.Entry({id,enable:true,protocol:'vless',port:19000+id,remark:id===7?'US XHTTP':'REALITY',settings:JSON.stringify({clients:[{id:'same-uuid',email:`entry-${id}`}]}),streamSettings:'{"network":"tcp","security":"none"}',...extra});
const entries=[entry(7,{externalHost:'proxy.example.test',externalPort:443,externalTls:true}),entry(8),entry(9,{enable:false})];
function instance() {
    const w=new VueRuntime({...workspaceOptions,propsData:{entries:[...entries],listError:false,refresh:async()=>{}}});
    w.$message={success(){},warning(){},error(){}};
    return w;
}
function pageInstance() {
    const p={...pageOptions.data,$refs:{},$nextTick:callback=>callback(),$set:(object,key,value)=>object[key]=value,$message:{success(){},warning(){}},dbInbounds:[]};
    for (const [key,method] of Object.entries(pageOptions.methods)) p[key]=method.bind(p);
    for (const [key,getter] of Object.entries(pageOptions.computed)) Object.defineProperty(p,key,{get:()=>getter.call(p)});
    return p;
}
respond=async url=>{ reads.push(url);return {success:true,obj:{users:[{clientId:'same-uuid',enabled:true,effectiveEnabled:true,total:0,expiryTime:0,up:url.includes('/8/')?88:7,down:0}]}}; };
(async()=>{
    const p=pageInstance();p.setInbounds([...entries,entry(10,{archivedAt:1}),entry(11,{protocol:'socks'})]);
    assert.deepEqual(Array.from(p.userEntries,item=>item.id),[7,8,9]);
    assert.equal(p.hasUserEntries,true);assert.equal(entries[1].isManagedExternal,false);assert.equal(entries[1].isUserManageable,true);
    let selected;
    p.$refs.workspace={switchEntry:id=>selected=id,focusUsers(){}};p.showLegacyList=true;
    p.openUsers(entries[1]);assert.equal(selected,8);assert.equal(p.showLegacyList,false);
    p.filtersInitialized=true;location.search='?entry=8&view=entries&q=old';p.syncFiltersToUrl();assert.equal(location.search,'?entry=8');

    location.search='';storage.clear();
    let w=instance();w.restoreSelection();await w.load();assert.equal(w.entry.id,7);
    await w.switchEntry(8);await VueRuntime.nextTick();assert.equal(w.entry.id,8);assert.equal(w.policies[0].up,88);
    assert.equal(w.users[0].email,'entry-8');assert.equal(new URLSearchParams(location.search).get('entry'),'8');
    const selectionKey=w.selectionKey();assert.equal(storage.get(selectionKey),'8');
    w.query='specific';w.filter='disabled';w.persistPresentation();w.$destroy();
    w=instance();w.restoreSelection();w.restorePresentation();await w.load();assert.equal(w.entry.id,8);assert.equal(w.query,'specific');assert.equal(w.filter,'disabled');
    location.search='';const bare=instance();bare.restoreSelection();assert.equal(bare.entry.id,8,'bare route restores the previous selection');bare.$destroy();
    location.search='?entry=7';const linked=instance();linked.restoreSelection();assert.equal(linked.entry.id,7,'explicit link overrides session');linked.$destroy();
    location.search='?entry=999';storage.set(selectionKey,'999');const fallback=instance();fallback.restoreSelection();assert.equal(fallback.entry.id,7);fallback.$destroy();
    await w.switchEntry(9);assert.equal(w.entry.id,9,'disabled entries remain manageable');
    w.delivery={};await w.switchEntry(7);assert.equal(w.entry.id,9,'open editor blocks switching');w.delivery=null;
    await w.switchEntry(8);let finish;
    respond=url=>url.includes('/8/')?new Promise(resolve=>finish=resolve):Promise.resolve({success:true,obj:{users:[]}});
    const stale=w.load();await w.switchEntry(7);finish({success:true,obj:{users:[{clientId:'wrong',total:0,expiryTime:0}]}});await stale;
    assert.equal(w.snapshotId,7);assert.equal(w.policies.length,0,'late response cannot replace current-entry policies');
    w.editor={clientId:'same-uuid',email:'',enabled:true,limited:false,expiryValue:null};w.entries=[entries[1]];await VueRuntime.nextTick();await VueRuntime.nextTick();
    assert.equal(w.entry.id,8);assert.equal(w.editor,null,'removed entry invalidates its editor');assert.equal(storage.get(selectionKey),'8');
    // Finish the removal-triggered read before disposing the component.
    finish({success:true,obj:{users:[]}});await VueRuntime.nextTick();w.$destroy();

    const created=entry(12);p.dbInbounds=[...entries,created];p.ensureFirewallAccess=async()=>true;
    p.submit=async()=>({success:true,obj:{id:12}});p.inboundPayload=()=>({});p.showLegacyList=true;
    await p.addInbound({port:created.port},created);assert.equal(selected,12);assert.equal(p.showLegacyList,false);
    p.dbInbounds=[entry(8)];assert.equal(p.hasUserEntries,true,'ordinary VLESS works without any managed XHTTP');
    assert.ok(reads.some(url=>url.includes('/8/userPolicies')));
    console.log('PASS: all VLESS entries, unified user navigation, new-entry selection, URL/session restoration and precedence, disabled/deleted entries, editor protection, and real Vue request races');
})().catch(error=>{console.error(error);process.exit(1);});
