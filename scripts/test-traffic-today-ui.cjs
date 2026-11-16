const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const html = fs.readFileSync(path.join(__dirname, '../web/html/xui/index.html'), 'utf8');
const css = fs.readFileSync(path.join(__dirname, '../web/assets/css/traffic-today.css'), 'utf8');
let options;
function Vue(config) { options = config; }
Vue.component = () => {};
const sandbox = {Vue, siderDrawer: {}, toFixed: (value, precision) => Number(Number(value).toFixed(precision)), moment: require('../web/assets/moment/moment.min.js'), sizeFormat: value => `${value} B`};
vm.createContext(sandbox);
vm.runInContext(html.match(/<script>([\s\S]*)<\/script>/)[1], sandbox);
function view(today) {
    const context = {status: {monthlyTraffic: {today}}};
    context.isTodayTrafficDay = options.methods.isTodayTrafficDay.bind(context);
    return context;
}
const days = [
    {date: '2026-10-27', up: 123, estimated: true},
    {date: '2026-10-28', up: 456, estimated: false},
    {date: '2026-10-29', up: 789, estimated: false},
];
const current = view('2026-10-28');
assert.deepEqual(days.filter(current.isTodayTrafficDay).map(day => day.date), ['2026-10-28'], 'server date highlights the matching day, even when it is not the last column');
for (const today of [undefined, null, '', false, '2026-10-31', 'invalid']) {
    assert.equal(days.some(view(today).isTodayTrafficDay), false, 'missing or unmatched dates must not guess the last column');
}
const column = html.slice(html.indexOf('<div class="daily-traffic-column"'), html.indexOf('<a-empty v-else description="今日开始记录'));
const title = column.match(/:title="([^"]+)"/)[1];
const date = column.match(/class="daily-traffic-date">\[\[ ([\s\S]*?) \]\]/)[1];
const evaluate = expression => vm.runInContext(`(function(day){with(this){return ${expression};}})`, sandbox);
assert.equal(evaluate(date).call(current, days[1]), '今天');
assert.equal(evaluate(date).call(current, days[0]), '27');
assert.equal(evaluate(date).call(view(undefined), days[1]), '28');
assert.match(evaluate(title).call(current, days[1]), /10 月 28 日：456 B · 今日持续更新/);
assert.match(evaluate(title).call(current, days[0]), /10 月 27 日：约 123 B（按运行采样恢复）/);
const estimateToday = {...days[1], estimated: true};
assert.match(evaluate(title).call(current, estimateToday), /约 456 B（按运行采样恢复） · 今日持续更新/);
const before = JSON.stringify(days);
days.forEach(current.isTodayTrafficDay);
assert.equal(JSON.stringify(days), before, 'highlighting does not alter amounts, estimation flags or column order');
assert.equal(options.data.status.monthlyTraffic.today, '', 'no server metadata defaults to no highlight');
vm.runInContext('this.FixtureStatus = Status;', sandbox);
const payload = {cpu: 0, mem: {current: 0, total: 1}, disk: {current: 0, total: 1}, swap: {current: 0, total: 0}, loads: [0, 0, 0], netIO: {up: 0, down: 0}, netTraffic: {sent: 0, recv: 0}, xray: {state: 'running'}, monthlyTraffic: {today: '2026-10-28', daily: days}};
const model = new sandbox.FixtureStatus(payload);
assert.equal(model.monthlyTraffic.today, '2026-10-28', 'Status retains the server timezone metadata');
assert.equal(model.monthlyTraffic.daily, days, 'Status preserves the original daily values and estimated flags');
for (const selector of css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}]+)\{/g)) {
    assert.ok(selector[1].split(',').every(part => part.trim().includes('.daily-traffic-column.is-today')), 'all new styling is restricted to today columns');
}
assert.ok(html.includes('v-for="day in status.monthlyTraffic.daily"'), 'the original chart range and order remain');
assert.equal(html.includes('trafficRangeDays'), false, 'no previous dashboard redesign is restored');
console.log('PASS: exact server-date matching, absent/unmatched metadata, today labels and truthful estimated titles, unchanged values/order, and today-only CSS');
