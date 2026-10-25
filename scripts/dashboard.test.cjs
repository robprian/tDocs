const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const vm = require('node:vm');

const source = readFileSync(join(__dirname, '../internal/web/static/app.js'), 'utf8');

test('login inline script parses before users sign in', () => {
    const login = readFileSync(join(__dirname, '../internal/web/templates/login.html'), 'utf8');
    for (const [, script] of login.matchAll(/<script>([\s\S]*?)<\/script>/g)) {
        assert.doesNotThrow(() => new vm.Script(script));
    }
});

function dashboard(ids) {
    const nodes = Object.fromEntries(ids.map(id => [id, { innerHTML: '', textContent: '' }]));
    const context = vm.createContext({
        document: { addEventListener() {}, getElementById: id => nodes[id] || null },
        localStorage: { getItem: () => null },
        window: { fetch: async () => ({ ok: true, status: 200, json: async () => [] }) },
        console,
    });
    vm.runInContext(source, context);
    return { nodes, context };
}

test('quick access uses API counts and provides working category actions', () => {
    const { nodes, context } = dashboard(['ov-quick-access']);
    vm.runInContext('renderQuickAccess([{category:"document",files:7}, {category:"video",files:2}])', context);
    const html = nodes['ov-quick-access'].innerHTML;
    assert.match(html, /7 files/);
    assert.match(html, /2 files/);
    assert.equal((html.match(/0 files/g) || []).length, 2);
    for (const category of ['document', 'image', 'video', 'audio']) {
        assert.ok(html.includes(`openQuickAccess('${category}')`));
    }
});

test('metrics do not invent values when stats are missing', () => {
    const { nodes, context } = dashboard(['ov-stat-cards']);
    vm.runInContext('renderOverviewMetrics({})', context);
    assert.match(nodes['ov-stat-cards'].innerHTML, /Unavailable/);
    assert.doesNotMatch(nodes['ov-stat-cards'].innerHTML, /0%|Online/);
});

test('activity shows an honest empty state and escapes server content', async () => {
    const { nodes, context } = dashboard(['ov-activity']);
    await vm.runInContext('loadOverviewActivity()', context);
    assert.match(nodes['ov-activity'].innerHTML, /No activity yet/);
    context.window.fetch = async () => ({ ok: true, status: 200, json: async () => [{ action: '<script>x</script>', created_at: '2026-10-25T12:00:00Z' }] });
    await vm.runInContext('loadOverviewActivity()', context);
    assert.doesNotMatch(nodes['ov-activity'].innerHTML, /<script>/);
    assert.match(nodes['ov-activity'].innerHTML, /&lt;script&gt;/);
});

test('upload errors explain proxy limits without exposing raw HTML', async () => {
    const { context } = dashboard([]);
    context.response = { status: 413, text: async () => '<html><h1>413 Request Entity Too Large</h1></html>' };
    const message = await vm.runInContext('uploadErrorMessage(response, "Chunk failed")', context);
    assert.match(message, /413/);
    assert.match(message, /client_max_body_size/);
    assert.match(message, /16m/);
    assert.doesNotMatch(message, /<html>|<h1>/);
    context.response = { status: 502, text: async () => '<html>Bad gateway</html>' };
    const gateway = await vm.runInContext('uploadErrorMessage(response, "Upload failed")', context);
    assert.match(gateway, /502/);
    assert.doesNotMatch(gateway, /<html>/);
    context.response = { status: 400, text: async () => 'target folder not found' };
    assert.equal(await vm.runInContext('uploadErrorMessage(response, "Upload failed")', context), 'target folder not found');
});

test('a proxy 413 stops after one chunk attempt and shows a useful error', async () => {
    const { context } = dashboard([]);
    let attempts = 0;
    const notices = [];
    context.FormData = class { append() {} };
    context.setTimeout = fn => { fn(); return 0; };
    context.showToast = message => notices.push(message);
    context.renderUploadDrawer = () => {};
    context.apiFetch = async url => {
        if (url.endsWith('/init')) return { ok: true, json: async () => ({ id: 'test', chunk_size: 5 * 1024 * 1024 }) };
        assert.ok(url.endsWith('/chunk'), 'a blocked upload must not finalize');
        attempts++;
        return { ok: false, status: 413, text: async () => '<html>413</html>' };
    };
    vm.runInContext('uploadQueue = [{file: {name:"archive.zip",size:2000000,slice(){return {}}},status:"queued",nextChunk:0}];', context);
    await vm.runInContext('processNextUpload()', context);
    assert.equal(attempts, 1);
    assert.match(notices.join(' '), /client_max_body_size/);
    assert.doesNotMatch(notices.join(' '), /<html>/);
});
