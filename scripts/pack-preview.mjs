/**
 * Packs the built site into one self-contained HTML file.
 *
 * The site normally needs the Go binary beside it: the assets come from
 * there and so does every figure on both pages. This makes a copy that
 * needs nothing — assets inlined, and the API answered from a snapshot
 * taken off a running obfsweb — so it opens by double-clicking it, on a
 * machine with no Go, no Node and no network.
 *
 * It is for looking at and clicking through, not for deploying: the
 * figures are frozen at the moment the snapshot was taken.
 *
 * Usage: node scripts/pack-preview.mjs <snapshot-dir> <output.html>
 */
import { readFileSync, writeFileSync, readdirSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const repo = join(dirname(fileURLToPath(import.meta.url)), '..')
const snapshotDir = process.argv[2]
const out = process.argv[3]
if (!snapshotDir || !out) {
  console.error('usage: node scripts/pack-preview.mjs <snapshot-dir> <output.html>')
  process.exit(2)
}

const distDir = join(repo, 'web', 'dist')
const assetsDir = join(distDir, 'assets')
const assets = readdirSync(assetsDir)
const jsName = assets.find((f) => f.endsWith('.js'))
const cssName = assets.find((f) => f.endsWith('.css'))
if (!jsName || !cssName) throw new Error('no built assets in web/dist/assets — run npm run build first')

const js = readFileSync(join(assetsDir, jsName), 'utf8')
const css = readFileSync(join(assetsDir, cssName), 'utf8')
const favicon = readFileSync(join(repo, 'web', 'public', 'favicon.svg'), 'utf8')
let html = readFileSync(join(distDir, 'index.html'), 'utf8')

// -- the snapshot ---------------------------------------------------------

const read = (f) => JSON.parse(readFileSync(join(snapshotDir, f), 'utf8'))
const dashboards = {}
for (const f of readdirSync(snapshotDir)) {
  const m = /^dash-(.+)\.json$/.exec(f)
  if (m) dashboards[m[1]] = read(f)
}
const snapshot = {
  captured_at: read('status.json').generated_at,
  status: read('status.json'),
  locations: read('locations.json'),
  plans: read('plans.json'),
  dashboards,
}
const rangeCount = Object.keys(dashboards).length
if (rangeCount === 0) throw new Error('snapshot has no dashboard payloads')

// -- the two patches the bundle needs to run without a server -------------
//
// The router reads window.location.pathname, and on a file:// URL that is
// the path to this file. pushState there throws outright. So the route
// moves to the hash: it is the one part of a file:// URL a page may
// change. Both replacements are made against the whole function text, and
// each is asserted to match exactly once, so a rebuilt bundle that no
// longer contains them fails here rather than shipping a broken preview.

function patch(source, find, replace, what) {
  const n = source.split(find).length - 1
  if (n !== 1) {
    throw new Error(`${what}: expected exactly one match in the bundle, found ${n}. ` +
      `The bundle changed shape — update scripts/pack-preview.mjs.`)
  }
  return source.replace(find, replace)
}

let patched = js
patched = patch(
  patched,
  'function u(e){return e.replace(/\\/+$/,``)===`/dashboard`?`dashboard`:`landing`}',
  'function u(e){return window.__besyRoute===`dashboard`?`dashboard`:`landing`}',
  'route resolver',
)
patched = patch(
  patched,
  'function f(e){window.location.pathname!==e&&(window.history.pushState({},``,e),window.dispatchEvent(new PopStateEvent(`popstate`)))}',
  'function f(e){var r=e.replace(/\\/+$/,``)===`/dashboard`?`dashboard`:`landing`;' +
    'if(window.__besyRoute!==r){window.__besyRoute=r;' +
    'try{window.location.hash=r===`dashboard`?`#/dashboard`:`#/`}catch(_){}' +
    'window.dispatchEvent(new PopStateEvent(`popstate`))}}',
  'navigate',
)

// -- the shim that stands in for the server -------------------------------

const shim = `
(function () {
  var snapshot = window.__BESY_SNAPSHOT__
  var captured = Date.parse(snapshot.captured_at)
  var ISO = /^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}(\\.\\d+)?(Z|[+-]\\d{2}:\\d{2})$/

  // The figures are frozen, but their timestamps are not: every instant in
  // the snapshot moves forward by however long ago it was taken. Without
  // this the chart axes and the "обновлено в" clock would read as whatever
  // time it was when the snapshot was made, and the page would look stopped.
  function freshen(value, delta) {
    if (typeof value === 'string') {
      return ISO.test(value) ? new Date(Date.parse(value) + delta).toISOString().replace(/\\.\\d{3}Z$/, 'Z') : value
    }
    if (Array.isArray(value)) return value.map(function (v) { return freshen(v, delta) })
    if (value && typeof value === 'object') {
      var out = {}
      for (var k in value) out[k] = freshen(value[k], delta)
      return out
    }
    return value
  }

  function answer(path) {
    var delta = Date.now() - captured
    if (path.indexOf('/api/v1/status') === 0) return freshen(snapshot.status, delta)
    if (path.indexOf('/api/v1/locations') === 0) return snapshot.locations
    if (path.indexOf('/api/v1/plans') === 0) return snapshot.plans
    if (path.indexOf('/api/v1/dashboard') === 0) {
      var asked = (/[?&]range=([^&]*)/.exec(path) || [])[1]
      var id = asked ? decodeURIComponent(asked) : ''
      // The server answers an unknown window with the default rather than
      // an error, and the page reads back which one it used. Same here.
      var chosen = Object.prototype.hasOwnProperty.call(snapshot.dashboards, id) ? id : '6h'
      return freshen(snapshot.dashboards[chosen], delta)
    }
    return null
  }

  var realFetch = window.fetch ? window.fetch.bind(window) : null
  window.fetch = function (input, init) {
    var url = String(typeof input === 'string' ? input : (input && input.url) || '')
    var at = url.indexOf('/api/')
    if (at === -1) return realFetch ? realFetch(input, init) : Promise.reject(new TypeError('offline preview'))

    var body = answer(url.slice(at))
    if (body === null) {
      return Promise.resolve(new Response(JSON.stringify({ error: 'unknown endpoint' }), {
        status: 404, headers: { 'Content-Type': 'application/json' },
      }))
    }
    return Promise.resolve(new Response(JSON.stringify(body), {
      status: 200, headers: { 'Content-Type': 'application/json; charset=utf-8' },
    }))
  }

  // The route lives in the hash, which is the one part of a file:// URL a
  // page is allowed to change. Back and forward still work: the browser
  // fires hashchange, and the app is listening for popstate.
  function routeFromHash() {
    return /^#\\/?dashboard\\/?$/.test(window.location.hash) ? 'dashboard' : 'landing'
  }
  window.__besyRoute = routeFromHash()
  window.addEventListener('hashchange', function () {
    var next = routeFromHash()
    if (next === window.__besyRoute) return
    window.__besyRoute = next
    window.dispatchEvent(new PopStateEvent('popstate'))
  })
})()
`

// -- the preview's own page switcher --------------------------------------
//
// The product page carries no link to the dashboard — on the real site
// you reach it by its address. There is no address bar worth using in a
// file:// preview, so the preview brings its own switch. It is drawn to
// look like a tool rather than like the site: dashed, monospace, in the
// corner, and it says what it is.

const switcher = `    <div id="preview-switch" hidden>
      <span>предпросмотр</span>
      <button type="button" data-route="landing">Продукт</button>
      <button type="button" data-route="dashboard">Дашборд</button>
      <button type="button" data-close aria-label="Скрыть переключатель">×</button>
    </div>
    <style>
      #preview-switch {
        position: fixed; left: 12px; bottom: 12px; z-index: 9999;
        display: flex; align-items: center; gap: 6px; padding: 6px;
        border: 1px dashed currentColor; border-radius: 8px;
        background: Canvas; color: CanvasText; opacity: 0.92;
        font: 12px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      }
      #preview-switch[hidden] { display: none; }
      #preview-switch > span { padding: 0 4px; opacity: 0.6; }
      #preview-switch button {
        padding: 4px 8px; border: 1px solid currentColor; border-radius: 5px;
        background: none; color: inherit; font: inherit; cursor: pointer;
      }
      #preview-switch button[aria-current='true'] { background: CanvasText; color: Canvas; }
      @media print { #preview-switch { display: none; } }
    </style>
    <script>
      (function () {
        var bar = document.getElementById('preview-switch')
        var buttons = bar.querySelectorAll('button[data-route]')
        function sync() {
          var route = window.__besyRoute === 'dashboard' ? 'dashboard' : 'landing'
          buttons.forEach(function (b) {
            b.setAttribute('aria-current', String(b.dataset.route === route))
          })
        }
        buttons.forEach(function (b) {
          b.addEventListener('click', function () {
            window.location.hash = b.dataset.route === 'dashboard' ? '#/dashboard' : '#/'
          })
        })
        bar.querySelector('[data-close]').addEventListener('click', function () { bar.hidden = true })
        window.addEventListener('popstate', sync)
        sync()
        bar.hidden = false
      })()
    </script>`

// -- assemble -------------------------------------------------------------

// A </script> inside the JSON would end the tag it sits in.
const json = JSON.stringify(snapshot).replace(/</g, '\\u003c')

// Every replacement goes through a function rather than a string. A
// string replacement gives $& $` $' and $1 their special meanings, and
// the minified bundle is full of them — the first attempt at this
// spliced chunks of the page into the middle of its own script and
// produced a syntax error two hundred kilobytes in.
const put = (value) => () => value

html = html
  .replace(
    /<link rel="icon"[^>]*>/,
    put(`<link rel="icon" type="image/svg+xml" href="data:image/svg+xml;base64,${Buffer.from(favicon).toString('base64')}" />`),
  )
  .replace(
    /<script type="module" crossorigin src="[^"]*"><\/script>/,
    put(`<script>window.__BESY_SNAPSHOT__=${json};</script>\n    <script>${shim}</script>\n    <script type="module">${patched}</script>`),
  )
  .replace(/<link rel="stylesheet" crossorigin href="[^"]*">/, put(`<style>${css}</style>`))
  .replace('<div id="root"></div>', put(`<div id="root"></div>
    <noscript>Эта страница — приложение на JavaScript. Включите его, чтобы посмотреть.</noscript>
${switcher}`))

for (const leftover of [jsName, cssName, '/favicon.svg']) {
  if (html.includes(`"/assets/${leftover}"`) || html.includes(`"${leftover}"`)) {
    throw new Error(`${leftover} is still referenced by URL — the preview would need files beside it`)
  }
}

writeFileSync(out, html)
const kb = (Buffer.byteLength(html) / 1024).toFixed(0)
console.log(`Wrote ${out} — ${kb} kB, ${rangeCount} dashboard windows, snapshot from ${snapshot.captured_at}`)
