/* Unified entry workspace. Server policy remains authoritative for exports. */
const managedWorkspaceOptions = {
    template: '#managed-workspace-template',
    delimiters: ['[[', ']]'],
    props: ['entries', 'runtimeState', 'refresh', 'listError'],
    data() { return {
        selectedId: null, query: '', filter: 'all', sort: 'name',
        policies: [], loading: false, failed: false, hasSnapshot: false,
        requestSeq: 0, snapshotId: null, checkedAt: null, clock: Date.now(), timer: null,
        silentLoading: false, refreshTimer: null, disposed: false,
        editor: null, baseline: '', saving: false, saveError: '', delivery: null,
        pending: {}, exportErrors: {}, distributionOpenId: null, distributionPlacement: 'bottomLeft', certificate: null, certificateLoading: false, certificateError: false,
        subscriptionFeedback: {}, subscriptionFeedbackTimers: {}, exportRequests: {},
        presentationRestoring: false, restoreScrollTop: null,
        sizeFormat: value => sizeFormat(value).replace(/\b([KMGTPE])iB\b/g, '$1B').replace(/(\.\d*?[1-9])0+(?=\s)/g, '$1').replace(/\.0+(?=\s)/g, ''), moment,
    }; },
    computed: {
        entry() { return this.entries.find(x => x.id === this.selectedId) || this.entries.find(x => x.enable) || this.entries[0] || null; },
        users() {
            if (!this.entry) return [];
            try { return JSON.parse(this.entry.settings).clients || []; } catch (_) { return []; }
        },
        ready() { return !!this.entry && this.snapshotId === this.entry.id && this.hasSnapshot && !this.failed && !this.loading && !this.listError; },
        policiesById() { return new Map(this.policies.map(p => [p.clientId, p])); },
        policiesComplete() { return this.ready && this.users.every(u => this.policy(u)); },
        attentionCount() { return this.policiesComplete ? this.users.filter(u => this.needsAttention(u)).length : null; },
        availableCount() { return this.policiesComplete ? this.users.filter(u => this.status(u) === '可用').length : null; },
        filtering() { return !!this.query || this.filter !== 'all'; },
        filterStatusNote() {
            if (this.listError) return '上次读取的用户列表';
            if (this.filter === 'all' || !this.users.length) return '';
            if (this.failed) return this.hasSnapshot ? '上次读取的状态' : '用户状态读取失败';
            if (this.loading || !this.hasSnapshot) return '正在读取用户状态';
            if (!this.policiesComplete || (this.filter === 'active' && this.todayActiveCount === null)) return '部分用户状态待确认';
            return '';
        },
        emptyDescription() {
            if (this.filterStatusNote) return '暂时无法确认筛选结果';
            if (!this.users.length) return '这个入口还没有用户';
            if (this.query) return this.filter === 'all' ? '没有匹配这个名称的用户' : '当前筛选中没有匹配这个名称的用户';
            return {attention:'当前没有需要关注的用户', disabled:'当前没有手动停用的用户', active:'今天还没有用户产生流量'}[this.filter] || '没有符合条件的用户';
        },
        visibleUsers() {
            const query = this.query.toLocaleLowerCase();
            return this.users.filter(u => {
                if (query && !this.name(u).toLocaleLowerCase().includes(query)) return false;
                const p = this.policy(u);
                return this.filter === 'all' || (this.filter === 'attention' && this.needsAttention(u)) || (this.filter === 'disabled' && p && !p.enabled) || (this.filter === 'active' && this.isActiveToday(u));
            }).sort((a,b) => {
                const ap=this.policy(a), bp=this.policy(b);
                const byName=()=>this.name(a).localeCompare(this.name(b), 'zh-CN') || String(a.id).localeCompare(String(b.id));
                if (this.filter === 'attention') {
                    const priority=this.attentionPriority(a)-this.attentionPriority(b);
                    if (priority) return priority;
                    if (this.attentionPriority(a) === 1 && ap.expiryTime !== bp.expiryTime) return ap.expiryTime-bp.expiryTime;
                }
                if (this.sort === 'today') return (bp && Number.isFinite(bp.todayUsed) ? bp.todayUsed : -1)-(ap && Number.isFinite(ap.todayUsed) ? ap.todayUsed : -1) || byName();
                if (this.sort === 'traffic') return this.used(bp)-this.used(ap) || byName();
                if (this.sort === 'usage') {
                    // Unlimited accounts have no allowance percentage and follow limited accounts.
                    const av=ap && ap.total>0 ? this.usagePercent(ap) : -1;
                    const bv=bp && bp.total>0 ? this.usagePercent(bp) : -1;
                    return bv-av || byName();
                }
                if (this.sort === 'expiry') {
                    const av=ap && ap.expiryTime>0 ? ap.expiryTime : Infinity;
                    const bv=bp && bp.expiryTime>0 ? bp.expiryTime : Infinity;
                    return (av===bv ? 0 : av-bv) || byName();
                }
                return byName();
            });
        },
        todayTotal() {
            if (!this.policiesComplete || this.users.some(u => !Number.isFinite(this.policy(u).todayUsed))) return null;
            return this.users.reduce((sum,u) => sum+this.policy(u).todayUsed,0);
        },
        todayTotalHint() {
            if (this.todayTotal === null) return '北京时间；部分用户今日用量尚未获取';
            return this.users.some(u => this.policy(u).todayPartial) ? '北京时间；今日统计包含开始记录后的用量，尚非完整全天数据' : '北京时间今日 00:00 起，当前入口所有用户上传与下载用量合计';
        },
        todayActiveCount() {
            if (!this.ready || this.users.some(u => !this.policy(u) || !Number.isFinite(this.policy(u).lastActiveAt))) return null;
            return this.users.filter(u => this.isActiveToday(u)).length;
        },
        quotaPreview() {
            if (!this.editor) return '';
            const current=Number(this.editor.currentUsed || 0), total=Math.round(Number(this.editor.totalGB)*ONE_GB);
            if (!this.editor.limited) return `本周期已用 ${this.sizeFormat(current)}，保存后不限额。`;
            if (!Number.isSafeInteger(total) || total<=0) return '请填写大于 0 的每周期额度。';
            if (current>=total) return `本周期已用 ${this.sizeFormat(current)}，保存后额度为 ${this.sizeFormat(total)}，用户将因额度用尽而不可用。`;
            return `本周期已用 ${this.sizeFormat(current)}，保存后剩余 ${this.sizeFormat(total-current)}。`;
        },
        quotaPreviewWarning() {
            if (!this.editor || !this.editor.limited) return false;
            const total=Math.round(Number(this.editor.totalGB)*ONE_GB);
            return Number.isSafeInteger(total) && total>0 && Number(this.editor.currentUsed || 0)>=total;
        },
        expiryPreview() { return this.editor && this.editor.expiryValue ? this.editor.expiryValue.format('YYYY-MM-DD HH:mm:ss')+' 到期' : '无限期'; },
        cycleUp() { return Number(this.entry && this.entry.up || 0); },
        cycleDown() { return Number(this.entry && this.entry.down || 0); },
        cycleUsed() { return this.cycleUp + this.cycleDown; },
        cycleResetDay() { return this.entry && this.entry.trafficResetDay || (this.policies[0] && this.policies[0].resetDay) || 1; },
        sharedResetDay() {
            if (!this.users.length || this.users.some(u => !this.policy(u))) return null;
            const days = new Set(this.users.map(u => this.resetDay(this.policy(u))));
            return days.size === 1 ? [...days][0] : null;
        },
        editorResetDay() { return this.resetDay(this.editor && this.policy({id:this.editor.clientId})); },
        editorState() { return this.editor ? this.editorSignature(this.editor) : ''; },
        editorDirty() { return !!this.editor && this.editorState !== this.baseline; },
        runtimeIssue() {
            const state=this.runtimeState;
            if (!state) return '';
            if (state.error) return state.running ? '配置应用失败，当前仍在运行上次配置' : '配置应用失败，核心未运行';
            return state.running === false ? '核心未运行，用户暂时无法连接' : '';
        },
        cycleDescription() { return `本周期上传 ${this.sizeFormat(this.cycleUp)}，下载 ${this.sizeFormat(this.cycleDown)}，合计 ${this.sizeFormat(this.cycleUsed)}`; },
        runtimeLabel() {
            const state=this.runtimeState;
            if (!state) return '运行状态暂未获取';
            if (state.error) return state.running ? '应用失败 · 旧配置仍运行' : '应用失败 · 核心未运行';
            if (!state.running) return '核心未运行';
            return state.applied ? '核心运行中 · 配置已应用' : '已保存 · 正在应用';
        },
        runtimeColor() {
            const s=this.runtimeState;
            return !s ? undefined : (s.error || !s.running ? 'red' : s.applied ? 'green' : 'orange');
        },
        drawerVisible() { return !!this.editor || !!this.delivery; },
        certLabel() {
            if (this.certificateError) return '证书暂未获取';
            if (!this.certificate) return '证书未检查';
            return {valid:'证书有效',invalid:'证书校验失败',unreachable:'无法验证证书'}[this.certificate.state] || '证书暂未获取';
        },
    },
    watch: {
        editorState() { this.saveError=''; },
        query() { this.distributionOpenId=null;this.persistPresentation(); },
        filter() { this.distributionOpenId=null;this.persistPresentation(); },
        sort() { this.distributionOpenId=null;this.persistPresentation(); },
        'entry.id'(next, previous) {
            // Explicit switches reset synchronously. Handle an entry removed by
            // a list refresh here, including invalidating an open editor.
            if (next === previous || this.selectedId === next) return;
            this.resetUserActions();this.editor=null;this.delivery=null;
            this.selectedId=next || null;
            this.restorePresentation();this.persistSelection();this.load();
        },
        users() { this.pruneUserActions(); },
    },
    methods: {
        name(u) { return u.email || '未命名用户'; },
        initials(u) { return Array.from(this.name(u))[0].toLocaleUpperCase(); },
        policy(u) { return this.policiesById.get(u.id) || null; },
        used(row) { return row ? Number(row.up || 0)+Number(row.down || 0) : 0; },
        resetDay(p) { return p && Number.isInteger(p.resetDay) && p.resetDay >= 1 && p.resetDay <= 31 ? p.resetDay : this.cycleResetDay; },
        status(u) {
            if (this.policy(u) && (this.failed || this.listError)) return '状态待确认';
            return this.snapshotStatus(u);
        },
        snapshotStatus(u) {
            const p=this.policy(u);
            if (!p) return '暂未获取';
            if (!this.entry.enable) return '入口已停用';
            if (!p.enabled) return '手动停用';
            if (p.expiryTime && p.expiryTime <= this.clock) return '已到期';
            if (p.total > 0 && this.used(p) >= p.total) return '额度已用尽';
            return p.effectiveEnabled ? '可用' : p.reason || '不可用';
        },
        needsAttention(u) {
            // Keep the last policy snapshot in a filtered list when refreshing fails.
            // Display status, headline counts and exports still acknowledge stale data.
            const p=this.policy(u); if (!p || !p.enabled) return false;
            return this.snapshotStatus(u) !== '可用' || !!((p.expiryTime > this.clock && p.expiryTime <= this.clock+7*86400000) || (p.total > 0 && this.used(p)/p.total >= .9));
        },
        attentionPriority(u) {
            if (this.snapshotStatus(u)!=='可用') return 0;
            const p=this.policy(u);
            return p && p.expiryTime>this.clock && p.expiryTime<=this.clock+7*86400000 ? 1 : 2;
        },
        statusClass(u) { return !this.policy(u) || !this.policy(u).enabled || this.failed || this.listError ? 'is-muted' : this.status(u)==='可用' ? 'is-ok' : 'is-error'; },
        attentionHints(u) {
            const p=this.policy(u); if (!p || this.status(u)!=='可用') return [];
            const hints=[], expiry=this.expiryHint(p);
            if (expiry) hints.push(expiry);
            if (p.total>0 && this.usagePercent(p)>=90) hints.push(`额度剩余 ${Math.max(0,Math.round((100-this.usagePercent(p))*10)/10)}%`);
            return hints;
        },
        attentionHint(u) { return this.attentionHints(u).join(' · '); },
        isActiveToday(u) {
            const p=this.policy(u), start=new Date(this.clock);start.setHours(0,0,0,0);
            return !!p && Number.isFinite(p.lastActiveAt) && p.lastActiveAt>=start.getTime() && p.lastActiveAt<=this.clock;
        },
        expiry(p) { return p && p.expiryTime ? this.moment(p.expiryTime).format('YYYY-MM-DD') : '无限期'; },
        expiryHint(p) {
            if (!p || !p.expiryTime) return '';
            if (p.expiryTime <= this.clock) return '已到期';
            const days=Math.ceil((p.expiryTime-this.clock)/86400000);
            return days <= 7 ? `剩余 ${days} 天` : '';
        },
        usagePercent(p) { return p && p.total > 0 ? this.used(p)/p.total*100 : 0; },
        usagePercentLabel(p) {
            const n=this.usagePercent(p);
            return `${n > 0 ? Math.max(1,Math.round(n)) : 0}%`;
        },
        usageTone(p) { return this.usagePercent(p)>=100 ? 'is-exhausted' : this.usagePercent(p)>=90 ? 'is-warning' : ''; },
        usageSegment(p, key) {
            const used=this.used(p), denominator=p && p.total > 0 ? Math.max(p.total,used) : used;
            return denominator > 0 ? Number(p[key] || 0)/denominator*100 : 0;
        },
        remaining(p) { return p.total > 0 ? (this.used(p)>=p.total ? '额度已用尽' : '剩余 '+this.sizeFormat(p.total-this.used(p))) : '不限额'; },
        usageDescription(p) { return `已用 ${this.sizeFormat(this.used(p))}，${p.total>0 ? '额度 '+this.sizeFormat(p.total)+'，'+this.remaining(p) : '不限额'}；上传 ${this.sizeFormat(p.up)}，下载 ${this.sizeFormat(p.down)}`; },
        ringLength(value) { return this.cycleUsed > 0 ? value / this.cycleUsed * (2*Math.PI*26) : 0; },
        entryUserCount(entry) {
            try { const clients=JSON.parse(entry.settings).clients;return Array.isArray(clients) ? clients.length : 0; } catch (_) { return 0; }
        },
        selectionKey() { return `roylive:workspace:entry:v1:${location.pathname || ''}`; },
        restoreSelection() {
            let requested=null, saved=null;
            try { requested=Number(new URLSearchParams(location.search || '').get('entry')); } catch (_) {}
            try { saved=Number(window.sessionStorage.getItem(this.selectionKey())); } catch (_) {}
            const valid=id=>Number.isSafeInteger(id) && id>0 && this.entries.some(entry=>entry.id===id);
            this.selectedId=valid(requested) ? requested : valid(saved) ? saved : this.entry && this.entry.id;
            this.persistSelection();
        },
        persistSelection() {
            if (!this.entry || typeof window==='undefined') return;
            try { window.sessionStorage.setItem(this.selectionKey(),String(this.entry.id)); } catch (_) {}
            try {
                const params=new URLSearchParams(location.search || '');params.set('entry',String(this.entry.id));
                history.replaceState(null,'',`${location.pathname}?${params.toString()}${location.hash || ''}`);
            } catch (_) { /* Selection still works when storage or history is unavailable. */ }
        },
        presentationKey() { return this.entry ? `roylive:workspace:v4:${location.pathname || ''}:${this.entry.id}` : ''; },
        persistPresentation() {
            if (this.presentationRestoring || !this.entry || typeof window==='undefined') return;
            try {
                window.sessionStorage.setItem(this.presentationKey(),JSON.stringify({query:this.query.slice(0,256),filter:this.filter,sort:this.sort,scrollTop:Math.max(0,Number(window.scrollY || 0))}));
            } catch (_) { /* Presentation persistence is optional, including in private browsing. */ }
        },
        restorePresentation() {
            if (!this.entry || typeof window==='undefined') return;
            this.presentationRestoring=true;
            try {
                const saved=JSON.parse(window.sessionStorage.getItem(this.presentationKey()) || 'null');
                this.query=saved && typeof saved.query==='string' ? saved.query.slice(0,256) : '';
                this.filter=saved && ['all','attention','disabled','active'].includes(saved.filter) ? saved.filter : 'all';
                this.sort=saved && ['name','traffic','today','usage','expiry'].includes(saved.sort) ? saved.sort : 'name';
                this.restoreScrollTop=saved && Number.isFinite(saved.scrollTop) ? Math.max(0,saved.scrollTop) : null;
            } catch (_) { this.query='';this.filter='all';this.sort='name';this.restoreScrollTop=null; }
            this.$nextTick(()=>{this.presentationRestoring=false;});
        },
        restoreScroll() {
            const top=this.restoreScrollTop;this.restoreScrollTop=null;
            if (top===null || typeof window==='undefined') return;
            this.$nextTick(()=>{window.scrollTo({top,behavior:'auto'});});
        },
        setFilter(value) {
            if (!['all','attention','disabled','active'].includes(value)) return;
            this.query='';
            this.filter=value;
            this.$nextTick(()=>{
                const input=this.$refs.search;
                const node=input && (input.$el || input);
                if (node && node.scrollIntoView) node.scrollIntoView({block:'nearest',behavior:'auto'});
            });
        },
        clearSearch(event) {
            if (!this.query || event.isComposing || event.keyCode === 229) return;
            event.preventDefault();
            event.stopPropagation();
            this.query='';
        },
        editorSignature(form) {
            return JSON.stringify([
                form.email.trim(), form.enabled, form.limited,
                form.limited ? Math.round(Number(form.totalGB) * ONE_GB) : 0,
                form.expiryValue ? form.expiryValue.valueOf() : 0,
            ]);
        },
        async load(options = {}) {
            if (!this.entry) return;
            const id=this.entry.id, seq=++this.requestSeq;
            const silent=options && options.silent === true && this.hasSnapshot && this.snapshotId === id;
            if (!silent) this.distributionOpenId=null;
            if (this.snapshotId !== id) {
                this.resetUserActions();
                this.policies=[];this.hasSnapshot=false;this.checkedAt=null;this.certificate=null;this.certificateError=false;this.exportErrors={};
            }
            this.loading=!silent;this.silentLoading=silent;
            try {
                const msg=await HttpUtil.post(`/xui/inbound/${id}/userPolicies`);
                if (this.disposed || seq!==this.requestSeq || !this.entry || this.entry.id!==id) return;
                if (!msg.success || !msg.obj || !Array.isArray(msg.obj.users) || msg.obj.users.some(p => !Number.isFinite(p.total) || !Number.isFinite(p.expiryTime))) throw new Error('read');
                this.clock=Date.now();this.policies=msg.obj.users;this.snapshotId=id;this.hasSnapshot=true;this.checkedAt=Date.now();this.failed=false;
                this.restoreScroll();
                if (silent && this.$emit) this.$emit('refresh-runtime');
            } catch (_) { if (!this.disposed && seq===this.requestSeq) this.failed=true; }
            finally {
                if (!this.disposed && seq===this.requestSeq) {
                    this.loading=false;this.silentLoading=false;
                    if (this.$emit) this.$emit('updated',{checkedAt:this.checkedAt,failed:this.failed});
                }
            }
        },
        canRefreshSilently() {
            if (this.disposed || !this.entry || !this.hasSnapshot || this.snapshotId!==this.entry.id || typeof document==='undefined' || document.visibilityState!=='visible') return false;
            if (this.drawerVisible || this.saving || this.loading || this.silentLoading || this.distributionOpenId || Object.values(this.pending).some(Boolean)) return false;
            // Background reads must not move the list while another control is in use.
            const overlays=document.querySelectorAll('.ant-modal-wrap, .ant-dropdown, .ant-select-dropdown, .ant-calendar-picker-container');
            return !Array.from(overlays).some(node => node.getClientRects().length && (typeof getComputedStyle!=='function' || getComputedStyle(node).visibility!=='hidden'));
        },
        async refreshSilently() {
            if (this.canRefreshSilently()) await this.load({silent:true});
        },
        async switchEntry(id) {
            if (this.drawerVisible || !this.entries.some(entry=>entry.id===id)) return;
            if (!this.entry || this.entry.id!==id) this.resetUserActions();
            this.persistPresentation();this.selectedId=id;this.persistSelection();this.restorePresentation();this.restoreScrollTop=null;await this.load();
        },
        focusUsers() { this.$nextTick(() => { const node=this.$refs.search; if(node) node.focus(); }); },
        newClientId() {
            if (typeof crypto === 'undefined' || !crypto.getRandomValues) throw new Error('Secure random unavailable');
            if (crypto.randomUUID) return crypto.randomUUID();
            const bytes=crypto.getRandomValues(new Uint8Array(16));bytes[6]=(bytes[6]&15)|64;bytes[8]=(bytes[8]&63)|128;
            const hex=Array.from(bytes,b=>b.toString(16).padStart(2,'0')).join('');
            return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}`;
        },
        openUser(u) {
            if (!this.ready) return;
            this.distributionOpenId=null;
            const p=u && this.policy(u);
            if (u && !p) return;
            let newId='';
            if (!u) { try {newId=this.newClientId();} catch (_) {this.$message.error('无法安全生成用户标识，请使用支持加密随机数的浏览器。');return;} }
            this.delivery=null;this.saveError='';
            this.editor=u ? {clientId:u.id,email:u.email || '',enabled:p.enabled,limited:p.total>0,totalGB:p.total/ONE_GB,expiryValue:p.expiryTime ? this.moment(p.expiryTime) : null,currentUsed:this.used(p),legacyShared:p.groupId>0,create:false,expectedEmail:u.email || '',expectedEnabled:p.enabled,expectedTotal:p.total,expectedExpiryTime:p.expiryTime,expectedGroupId:p.groupId || 0}
                : {clientId:newId,email:'',enabled:true,limited:false,totalGB:0,expiryValue:null,currentUsed:0,create:true};
            this.baseline=this.editorState;
            if (u) this.manageHy('status');
        },
        copyHyLink() {userModal.copyText(this.editor.hy.link,'HY2 链接已复制');},
        showHyQR() {qrModal.show(this.editor.hy.name || 'Hysteria 2',this.editor.hy.link);},
        async manageHy(action) {
            const editor=this.editor;
            if (!editor || editor.create || editor.hyBusy) return;
            this.$set(editor,'hyBusy',true);
            try {
                const msg=await HttpUtil.post('/xui/inbound/'+this.entry.id+'/hysteria',{clientId:editor.clientId,action});
                if (this.editor!==editor || !msg.success) return;
                this.$set(editor,'hy',msg.obj);
            } finally {this.$set(editor,'hyBusy',false);}
        },
        setExpiryShortcut(mode) {
            if (!this.editor || this.saving || this.editor.legacyShared) return;
            if (mode==='unlimited') {this.editor.expiryValue=null;return;}
            if (mode!=='30' && mode!=='90') return;
            const expiry=this.editor.expiryValue ? this.editor.expiryValue.valueOf() : 0;
            const base=Math.max(Date.now(),Number.isFinite(expiry) ? expiry : 0);
            this.editor.expiryValue=this.moment(base+Number(mode)*86400000);
        },
        closeUser() {
            if (this.saving && !this.delivery) return;
            if (this.editorDirty) {
                this.$confirm({title:'放弃未保存的更改？',content:'用户名称、状态与个人额度尚未保存。',okText:'放弃更改',cancelText:'继续编辑',onOk:()=>{this.editor=null;}});
            } else { this.editor=null;this.delivery=null; }
        },
        async saveUser() {
            if (!this.editor || this.saving || (!this.editor.create && !this.editorDirty)) return;
            const total=this.editor.limited ? Math.round(Number(this.editor.totalGB)*ONE_GB) : 0;
            if (!Number.isSafeInteger(total) || total < 0 || (this.editor.limited && total === 0)) {
                this.saveError='请填写大于 0 的有效个人额度，或关闭额度限制。';
                if (this.$refs.editorQuota) this.$refs.editorQuota.focus();
                return;
            }
            const expiryTime=this.editor.expiryValue ? this.editor.expiryValue.valueOf() : 0;
            if (!Number.isSafeInteger(expiryTime) || expiryTime < 0) {
                this.saveError='请填写有效的到期时间。';
                if (this.$refs.editorExpiry) this.$refs.editorExpiry.focus();
                return;
            }
            this.saving=true;this.saveError='';
            const {limited,totalGB,expiryValue,currentUsed,legacyShared,hy,hyBusy,...fields}=this.editor;
            const form={...fields,total,expiryTime};
            let committed=false;
            try {
                const msg=await HttpUtil.post(`/xui/inbound/${this.entry.id}/user/save`,form);
                if (!msg.success) { this.saveError=msg.msg || '保存失败，请重试。';return; }
                committed=true;
                this.editor=null;
                if (form.create) this.delivery={id:form.clientId,email:form.email};
                else this.$message.success('用户已保存');
                await this.refresh();
                app.refreshRuntimeAfterSave();
            } catch (_) {
                if (committed) this.$message.warning('用户已保存，状态刷新失败，请重新读取。');
                else this.saveError='保存未确认，输入已保留。请刷新核对后重试。';
            }
            finally {this.saving=false;}
        },
        removeUser(u) {
            if (!this.ready) return;
            const p=this.policy(u);if (!p) return;
            const entryId=this.entry.id;
            this.$confirm({title:`删除“${this.name(u)}”？`,content:'此用户将无法连接，原订阅停止提供配置。历史用量保留。',okText:'删除用户',okType:'danger',cancelText:'取消',onOk:async()=>{
                const msg=await HttpUtil.post(`/xui/inbound/${entryId}/user/save`,{clientId:u.id,email:u.email || '',enabled:p.enabled,total:p.total,expiryTime:p.expiryTime,delete:true,expectedEmail:u.email || '',expectedEnabled:p.enabled,expectedTotal:p.total,expectedExpiryTime:p.expiryTime,expectedGroupId:p.groupId || 0});
                if(msg.success){this.clearUserAction(u,entryId);await this.refresh();app.refreshRuntimeAfterSave();}
            }});
        },
        userActionKey(u, entryId=this.entry && this.entry.id) { return JSON.stringify([entryId,u.id]); },
        subscriptionCopied(u) { return !this.disposed && !!this.entry && !!this.subscriptionFeedback[this.userActionKey(u)] && this.users.some(user=>user.id===u.id); },
        clearSubscriptionFeedback(key) {
            const record=this.subscriptionFeedbackTimers[key];
            if (record) clearTimeout(record.timer);
            delete this.subscriptionFeedbackTimers[key];
            this.$delete(this.subscriptionFeedback,key);
        },
        showSubscriptionFeedback(u, entryId) {
            if (this.disposed || !this.entry || this.entry.id!==entryId || !this.users.some(user=>user.id===u.id)) return;
            const key=this.userActionKey(u,entryId);
            this.clearSubscriptionFeedback(key);
            this.$set(this.subscriptionFeedback,key,true);
            const record={timer:null};
            this.subscriptionFeedbackTimers[key]=record;
            record.timer=setTimeout(()=>{
                // A canceled callback may already be queued; it cannot clear a newer confirmation.
                if (this.subscriptionFeedbackTimers[key]!==record) return;
                delete this.subscriptionFeedbackTimers[key];
                if (!this.disposed) this.$delete(this.subscriptionFeedback,key);
            },1500);
        },
        clearUserAction(u, entryId=this.entry && this.entry.id) {
            const key=this.userActionKey(u,entryId);
            this.clearSubscriptionFeedback(key);
            delete this.exportRequests[key];
            if (this.entry && this.entry.id===entryId) {
                this.$delete(this.pending,u.id);this.$delete(this.exportErrors,u.id);
            }
        },
        pruneUserActions() {
            const valid=new Set(this.users.map(u=>this.userActionKey(u)));
            for (const key of Object.keys(this.subscriptionFeedbackTimers)) if (!valid.has(key)) this.clearSubscriptionFeedback(key);
            for (const key of Object.keys(this.exportRequests)) if (!valid.has(key)) delete this.exportRequests[key];
            const userIds=new Set(this.users.map(u=>u.id));
            for (const id of Object.keys(this.pending)) if (!userIds.has(id)) this.$delete(this.pending,id);
            for (const id of Object.keys(this.exportErrors)) if (!userIds.has(id)) this.$delete(this.exportErrors,id);
        },
        resetUserActions() {
            for (const key of Object.keys(this.subscriptionFeedbackTimers)) this.clearSubscriptionFeedback(key);
            this.subscriptionFeedback={};this.exportRequests={};this.pending={};this.exportErrors={};
        },
        canExport(u) { return this.ready && this.status(u)==='可用' && !this.pending[u.id]; },
        setDistributionVisible(u,visible) {
            if(visible && this.canExport(u)){this.positionDistribution(u);this.distributionOpenId=u.id;}
            else if(this.distributionOpenId===u.id)this.distributionOpenId=null;
        },
        openDistributionMenu(u) {
            if(!this.canExport(u))return;
            this.positionDistribution(u);
            this.distributionOpenId=u.id;
            const focus=()=>{
                if(this.distributionOpenId!==u.id)return;
                const refs=this.$refs['distribution-menu-'+u.id], menu=Array.isArray(refs) ? refs[0] : refs;
                const node=menu && (menu.$el || menu), item=node && node.querySelector('[role="menuitem"]:not(.ant-dropdown-menu-item-disabled)');
                if(item)item.focus();else if(node && node.focus)node.focus();
            };
            this.$nextTick(()=>{if(typeof window!=='undefined' && window.requestAnimationFrame)window.requestAnimationFrame(focus);else focus();});
        },
        closeDistribution(u,restoreFocus=false) {
            this.distributionOpenId=null;
            if (restoreFocus) this.$nextTick(()=>{
                const refs=this.$refs['distribution-'+u.id], trigger=Array.isArray(refs) ? refs[0] : refs;
                const node=trigger && (trigger.$el || trigger);if(node && node.focus)node.focus();
            });
        },
        distributeChoice(u,action) { this.closeDistribution(u);return this.distribute(u,action); },
        positionDistribution(u) {
            const refs=this.$refs['distribution-'+u.id], ref=Array.isArray(refs) ? refs[0] : refs;
            const node=ref && (ref.$el || ref);
            this.distributionPlacement='bottomLeft';
            if(node && node.getBoundingClientRect && typeof window!=='undefined' && Number.isFinite(window.innerHeight)){
                const rect=node.getBoundingClientRect();
                if(window.innerHeight-rect.bottom<176 && rect.top>=176)this.distributionPlacement='topLeft';
            }
        },
        resizeDistribution() {
            const user=this.users.find(u=>u.id===this.distributionOpenId);
            if(user)this.$nextTick(()=>this.positionDistribution(user));
        },
        distributionKey(u,event) {
            if(!['ArrowDown','ArrowUp','Home','End','Enter',' ','Escape','Tab'].includes(event.key))return;
            event.preventDefault();event.stopPropagation();
            if(event.key==='Escape'){this.closeDistribution(u,true);return;}
            if(event.key==='Tab'){
                this.closeDistribution(u);
                this.$nextTick(()=>{
                    const refs=this.$refs['distribution-'+u.id], ref=Array.isArray(refs) ? refs[0] : refs;
                    const node=ref && (ref.$el || ref), group=node && node.closest('.mw-compact-actions');
                    const next=group && group.querySelector(event.shiftKey ? '.mw-copy-primary' : '.mw-edit');
                    if(next && !next.disabled)next.focus();
                });return;
            }
            if(!this.canExport(u)){this.closeDistribution(u,true);return;}
            const items=Array.from(event.currentTarget.querySelectorAll('[data-distribution-action]')).filter(item=>!item.classList.contains('ant-dropdown-menu-item-disabled'));
            if(!items.length)return;
            const current=items.indexOf(document.activeElement);
            if(event.key==='Enter' || event.key===' '){this.distributeChoice(u,items[Math.max(0,current)].getAttribute('data-distribution-action'));return;}
            const index=event.key==='Home' ? 0 : event.key==='End' ? items.length-1 : event.key==='ArrowDown' ? (current+1)%items.length : (current<=0 ? items.length-1 : current-1);
            items[index].focus();
        },
        exportHint(u) {
            if (this.failed || this.listError) return '状态待确认，请刷新后再分发配置';
            if (!this.hasSnapshot || !this.policy(u)) return '用户状态暂未获取';
            if (this.loading) return '正在刷新用户状态';
            if (this.pending[u.id]) return '';
            const state=this.status(u);
            if (state==='可用') return '';
            return {'入口已停用':'入口已停用，启用入口后可分发配置','手动停用':'用户已停用，启用后可分发配置','已到期':'用户已到期，延长有效期后可分发配置','额度已用尽':'额度已用尽，调整额度或等待周期重置后可分发配置'}[state] || '用户当前不可用，请检查状态后重试';
        },
        async distribute(u,action) {
            if(this.disposed || !this.canExport(u) || !['subscription','link','qr','yaml'].includes(action))return;
            this.distributionOpenId=null;
            const id=this.entry.id;
            const key=this.userActionKey(u,id), request={};
            this.exportRequests[key]=request;
            const current=()=>!this.disposed && this.entry && this.entry.id===id && this.exportRequests[key]===request && this.users.some(user=>user.id===u.id);
            if(action==='subscription')this.clearSubscriptionFeedback(key);
            this.$set(this.pending,u.id,true);
            this.$delete(this.exportErrors,u.id);
            try{
                const subscription=action==='subscription';
                const msg=await HttpUtil.post(`/xui/inbound/${id}/${subscription?'userSubscription':'userProfile'}`,{clientId:u.id,host:location.hostname,name:this.name(u)});
                if(!current())return;
                if(!msg.success || !msg.obj)throw new Error('profile');
                if(subscription){
                    if(!msg.obj.url)throw new Error('profile');
                    if(await userModal.copyText(msg.obj.url,'订阅地址已复制',false)===false)throw new Error('copy');
                    if(current())this.showSubscriptionFeedback(u,id);
                    return;
                }
                const profile=msg.obj;
                if(action!=='yaml' && !profile.link)throw new Error('profile');
                if(action==='link' && await userModal.copyText(profile.link,'节点链接已复制')===false)throw new Error('copy');
                if(action==='qr')qrModal.show(((profile.connection && profile.connection.name) || this.entry.remark || this.name(u))+' · 二维码',profile.link);
                if(action==='yaml'){
                    if(!profile.clashVergeConfig || !profile.fileName)throw new Error('profile');
                    const url=URL.createObjectURL(new Blob([profile.clashVergeConfig],{type:'text/yaml;charset=utf-8'}));
                    const anchor=document.createElement('a');anchor.href=url;anchor.download=profile.fileName;
                    document.body.appendChild(anchor);anchor.click();anchor.remove();URL.revokeObjectURL(url);
                    this.$message.success('配置已下载');
                }
            }catch(error){
                if(!current())return;
                const copyFailed=error && error.message==='copy';
                const message=copyFailed ? '复制失败，请重试或下载 Clash 配置' : '获取连接配置失败，请重试';
                this.$set(this.exportErrors,u.id,message);if(!copyFailed)this.$message.error(message);
            }finally{
                if(this.exportRequests[key]===request){delete this.exportRequests[key];this.$delete(this.pending,u.id);}
            }
        },
        async checkCertificate() {
            if(this.certificateLoading)return;
            const id=this.entry.id;this.certificateLoading=true;this.certificateError=false;
            try{
                const msg=await HttpUtil.post(`/xui/inbound/${id}/endpointStatus`);
                if(!this.entry || this.entry.id!==id)return;
                if(!msg.success || !msg.obj){this.certificateError=true;return;}
                this.certificate=msg.obj;
            }catch(_){if(this.entry && this.entry.id===id)this.certificateError=true;}
            finally{this.certificateLoading=false;}
        },
        beforeUnload(event) {
            this.persistPresentation();
            if(this.editorDirty) {event.preventDefault();event.returnValue='';}
        },
    },
    mounted() { this.restoreSelection();this.restorePresentation();this.load();this.timer=setInterval(()=>{this.clock=Date.now();},30000);this.refreshTimer=setInterval(()=>{this.refreshSilently();},45000);window.addEventListener('beforeunload',this.beforeUnload);window.addEventListener('pagehide',this.persistPresentation);window.addEventListener('resize',this.resizeDistribution); },
    beforeDestroy() {this.persistPresentation();this.disposed=true;this.requestSeq++;this.resetUserActions();clearInterval(this.timer);clearInterval(this.refreshTimer);window.removeEventListener('beforeunload',this.beforeUnload);window.removeEventListener('pagehide',this.persistPresentation);window.removeEventListener('resize',this.resizeDistribution);},
};
Vue.component('managed-workspace',managedWorkspaceOptions);
