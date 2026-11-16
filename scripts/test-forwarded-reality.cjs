const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const assert = require('node:assert/strict');
const sandbox = {console, URL, location:{hostname:'panel.example.test'}};
vm.createContext(sandbox);
for (const file of ['web/assets/js/util/common.js','web/assets/js/util/utils.js','web/assets/js/model/xray.js','web/assets/js/model/models.js']) {
  vm.runInContext(fs.readFileSync(path.join(__dirname,'..',file),'utf8'),sandbox);
}
vm.runInContext(`
const entry=new DBInbound({id:8,protocol:'vless',port:8443,listen:'127.0.0.1',externalHost:'node.example.com',externalPort:443,externalTls:false,
 settings:JSON.stringify({clients:[{id:'11111111-1111-4111-8111-111111111111',flow:'xtls-rprx-vision'}],decryption:'none'}),
 streamSettings:JSON.stringify({network:'tcp',security:'reality',realitySettings:{serverNames:['target.example.com'],publicKey:'test-public-key',shortIds:['0123456789abcdef']}}),sniffing:'{}'});
this.link=entry.genLink();this.label=entry.transportLabel;this.endpoint=entry.connectionLabel;
`,sandbox);
const link=new URL(sandbox.link);
assert.equal(link.host,'node.example.com:443');
assert.equal(link.searchParams.get('security'),'reality');
assert.equal(link.searchParams.get('type'),'tcp');
assert.equal(link.searchParams.get('flow'),'xtls-rprx-vision');
assert.equal(link.searchParams.get('sni'),'target.example.com');
assert.equal(sandbox.label,'TCP＋REALITY');
assert.equal(sandbox.endpoint,'node.example.com:443');
assert.ok(!sandbox.link.includes('8443') && !sandbox.link.includes('127.0.0.1'));
console.log('PASS: forwarded REALITY browser links use public 443 and preserve Vision, REALITY and SNI');
