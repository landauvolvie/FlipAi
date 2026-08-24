package main

import (
	"net/http"
	"strings"
)

// The desktop UI ships its stylesheet and script as ordinary local assets
// instead of inlining them into every page. They contain no configuration and
// no secrets, they are identical for every page, and serving them separately
// keeps the page templates readable.

const uiCSS = `
:root{
  --bg:#ffffff;
  --bg-side:#fbfbfc;
  --surface:#ffffff;
  --surface-2:#f9fafb;
  --ink:#101828;
  --ink-2:#344054;
  --muted:#667085;
  --line:#e6e7eb;
  --line-soft:#f0f1f3;
  --brand:#6941c6;
  --brand-strong:#5a35ad;
  --brand-ink:#5a35ad;
  --brand-soft:#f4f0ff;
  --ok:#067647; --ok-soft:#ecfdf3;
  --warn:#b54708; --warn-soft:#fffaeb;
  --bad:#b42318; --bad-soft:#fef3f2;
  --info:#175cd3; --info-soft:#eff8ff;
  --shadow:0 1px 2px rgba(16,24,40,.05);
  --radius:10px;
  --pad:20px;
}
:root[data-theme="dark"]{
  --bg:#17171b;
  --bg-side:#131316;
  --surface:#1d1d22;
  --surface-2:#232329;
  --ink:#f4f5f7;
  --ink-2:#d5d7dd;
  --muted:#9aa1ad;
  --line:#2c2c34;
  --line-soft:#26262d;
  --brand:#9b7bf0;
  --brand-strong:#b09bf5;
  --brand-ink:#c4b3f8;
  --brand-soft:#2a2340;
  --ok:#5fd4a0; --ok-soft:#12291f;
  --warn:#f0b25f; --warn-soft:#2c2313;
  --bad:#f79e94; --bad-soft:#2e1815;
  --info:#8ab6f5; --info-soft:#152537;
  --shadow:none;
}
@media(prefers-color-scheme:dark){
  :root:not([data-theme="light"]):not([data-theme="dark"]){
    --bg:#17171b; --bg-side:#131316; --surface:#1d1d22; --surface-2:#232329;
    --ink:#f4f5f7; --ink-2:#d5d7dd; --muted:#9aa1ad; --line:#2c2c34; --line-soft:#26262d;
    --brand:#9b7bf0; --brand-strong:#b09bf5; --brand-ink:#c4b3f8; --brand-soft:#2a2340;
    --ok:#5fd4a0; --ok-soft:#12291f; --warn:#f0b25f; --warn-soft:#2c2313;
    --bad:#f79e94; --bad-soft:#2e1815; --info:#8ab6f5; --info-soft:#152537; --shadow:none;
  }
}
:root[data-compact="1"]{--pad:14px;--radius:8px}

*{box-sizing:border-box}
html,body{height:100%}
body{
  margin:0;background:var(--bg);color:var(--ink);
  font:14px/1.5 "Segoe UI Variable Text","Segoe UI",Inter,system-ui,-apple-system,sans-serif;
  -webkit-font-smoothing:antialiased;
}
button,input,select,textarea{font:inherit;color:inherit}
a{color:var(--brand-ink)}
h1,h2,h3{margin:0;letter-spacing:-.2px}

/* ---------- shell ---------- */
.app{display:grid;grid-template-columns:232px minmax(0,1fr);min-height:100vh}
.sidebar{
  background:var(--bg-side);border-right:1px solid var(--line);
  padding:18px 14px;display:flex;flex-direction:column;gap:4px;
  position:sticky;top:0;height:100vh;
}
.brand{display:flex;align-items:center;gap:10px;padding:6px 8px 22px;font-size:17px;font-weight:650}
.brand-mark{
  width:30px;height:30px;border-radius:8px;background:var(--brand);color:#fff;
  display:grid;place-items:center;font-weight:800;font-size:15px;flex:0 0 auto;
}
.nav{display:flex;flex-direction:column;gap:2px}
.nav a{
  display:flex;align-items:center;gap:11px;padding:9px 11px;border-radius:8px;
  color:var(--ink-2);text-decoration:none;font-size:13.5px;font-weight:500;
}
.nav a svg{width:18px;height:18px;flex:0 0 auto;opacity:.85}
.nav a:hover{background:var(--brand-soft);color:var(--brand-ink)}
.nav a[aria-current="page"]{background:var(--brand-soft);color:var(--brand-ink);font-weight:650}
.nav a[aria-current="page"] svg{opacity:1}
.nav-divider{height:1px;background:var(--line);margin:12px 6px}
.side-status{
  margin-top:auto;border:1px solid var(--line);border-radius:var(--radius);
  background:var(--surface);padding:13px;font-size:12px;color:var(--muted);
}
.side-status b{display:flex;align-items:center;gap:7px;color:var(--ink);font-size:12.5px;font-weight:600}
.side-status span{display:block;margin-top:5px}
.dot{width:8px;height:8px;border-radius:50%;background:#12b76a;flex:0 0 auto}
.dot.paused{background:#f79009}
.dot.stopped{background:#f04438}

.content{padding:30px 34px 46px;min-width:0;max-width:1340px}
.page-head{display:flex;justify-content:space-between;align-items:flex-start;gap:20px;margin-bottom:22px}
.page-head h1{font-size:30px;font-weight:640;display:flex;align-items:center;gap:12px}
.page-head p{margin:7px 0 0;color:var(--muted);font-size:13.5px;max-width:760px}
.page-actions{display:flex;gap:9px;flex-wrap:wrap;padding-top:4px}

/* ---------- tiles ---------- */
.tiles{display:grid;grid-template-columns:repeat(auto-fit,minmax(196px,1fr));gap:13px;margin-bottom:20px}
.tile{
  background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);
  padding:var(--pad);box-shadow:var(--shadow);min-width:0;
}
.tile-top{display:flex;align-items:flex-start;justify-content:space-between;gap:10px}
.tile-icon{
  width:34px;height:34px;border-radius:9px;background:var(--surface-2);border:1px solid var(--line);
  display:grid;place-items:center;color:var(--muted);flex:0 0 auto;
}
.tile-icon svg{width:18px;height:18px}
.tile-icon.brandish{background:var(--brand-soft);border-color:transparent;color:var(--brand-ink)}
.tile h3{font-size:14px;font-weight:620;margin-top:12px;line-height:1.35}
.tile .val{margin-top:3px;font-size:14px;font-weight:620;line-height:1.35;overflow-wrap:anywhere}
.tile .sub{margin-top:3px;font-size:12px;color:var(--muted);overflow-wrap:anywhere}
.tile .val.ok{color:var(--ok)}
.tile .val.warn{color:var(--warn)}
.tile .val.bad{color:var(--bad)}
.tile .val.brand{color:var(--brand-ink)}
.tile .val.big{font-size:21px;font-weight:650;margin-top:5px;line-height:1.3}
b.ok{color:var(--ok)}b.warn{color:var(--warn)}b.bad{color:var(--bad)}
#icon-sprites{display:none}
.hint{margin:6px 0 0;font-size:11.5px;color:var(--muted)}
.reconnect{position:fixed;left:50%;top:18px;transform:translateX(-50%);z-index:80;display:flex;align-items:center;gap:9px;padding:10px 16px;border-radius:999px;background:var(--warn-soft);color:var(--warn);border:1px solid var(--warn);font-size:12.5px;font-weight:600;box-shadow:0 10px 24px rgba(16,24,40,.14)}
.tile-foot{display:flex;justify-content:space-between;gap:10px;align-items:center;margin-top:13px;padding-top:11px;border-top:1px solid var(--line-soft);font-size:12px;color:var(--muted)}
.check{color:var(--ok);display:grid;place-items:center}
.check svg{width:19px;height:19px}
.check.warn{color:var(--warn)}
.check.bad{color:var(--bad)}

/* ---------- cards ---------- */
.card{
  background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);
  box-shadow:var(--shadow);margin-bottom:16px;overflow:hidden;
}
.card-head{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;padding:var(--pad)}
.card-head + .card-body{padding-top:0}
.card-head.divided{border-bottom:1px solid var(--line-soft)}
.card-head.divided + .card-body{padding-top:var(--pad)}
.card-head h2{font-size:16px;font-weight:640}
.card-head p{margin:4px 0 0;color:var(--muted);font-size:12.5px}
.card-head .head-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap}
.card-body{padding:var(--pad)}
.card-body > :first-child{margin-top:0}
.card-title-row{display:flex;align-items:center;gap:12px}
.card-title-row .mark{width:40px;height:40px;border-radius:9px;display:grid;place-items:center;font-weight:800;font-size:14px;flex:0 0 auto}
.mark.mail{background:var(--info-soft);color:var(--info)}
.mark.shield{background:var(--brand-soft);color:var(--brand-ink)}

/* ---------- brand marks ---------- */
.bmark{
  width:34px;height:34px;border-radius:9px;flex:0 0 auto;
  display:grid;place-items:center;overflow:hidden;
}
.bmark svg{width:20px;height:20px}
.bmark .glyph{font-weight:800;font-size:13px;line-height:1;letter-spacing:.5px}
.bmark.google{background:#fff;border:1px solid var(--line)}
:root[data-theme="dark"] .bmark.google{background:#f7f8fa}
.bmark.voice{background:#e8f0fe;border:1px solid transparent}
:root[data-theme="dark"] .bmark.voice{background:#1b2a44}
.bmark.codex{background:#0d1117;color:#fff}
.bmark.codex .glyph{font-family:ui-monospace,Consolas,monospace;font-size:12px}
.bmark.claude{background:#d97757;color:#fff}
.bmark.lg{width:40px;height:40px;border-radius:10px}
.bmark.lg svg{width:24px;height:24px}
.bmark.lg .glyph{font-size:15px}
.bmark.sm{width:22px;height:22px;border-radius:6px}
.bmark.sm svg{width:14px;height:14px}
.bmark.sm .glyph{font-size:9px}
.cards-2{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}
@media(max-width:1000px){.cards-2{grid-template-columns:minmax(0,1fr)}}
.cards-2 .card{margin-bottom:0}

/* ---------- rows ---------- */
.rows{display:flex;flex-direction:column}
.row{display:flex;justify-content:space-between;align-items:center;gap:16px;padding:13px 0;border-top:1px solid var(--line-soft)}
.row:first-child{border-top:0}
.row .label{font-size:13px;font-weight:560;color:var(--ink-2)}
.row .label span{display:block;font-weight:400;color:var(--muted);font-size:12px;margin-top:2px}
.row .value{display:flex;align-items:center;gap:10px;font-size:13px;color:var(--muted);text-align:right;overflow-wrap:anywhere}
.row .value b{color:var(--ink);font-weight:580}
.row .value b.ok{color:var(--ok)}
.row .value b.warn{color:var(--warn)}
.row .value b.bad{color:var(--bad)}

/* ---------- forms ---------- */
.field{margin-top:15px;min-width:0}
.field:first-child{margin-top:0}
.field label{display:block;font-size:12.5px;font-weight:580;color:var(--ink-2);margin-bottom:6px}
.field .hint{margin:6px 0 0;font-size:11.5px;color:var(--muted)}
.grid-2{display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:16px}
.grid-3{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:16px}
select{
  appearance:none;-webkit-appearance:none;padding-right:34px;cursor:pointer;
  background-image:url("data:image/svg+xml;charset=utf-8,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 20 20' fill='none' stroke='%23667085' stroke-width='1.8' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m5.5 8 4.5 4.5L14.5 8'/%3E%3C/svg%3E");
  background-repeat:no-repeat;background-position:right 9px center;background-size:16px;
}
.nicewrap{position:relative}
.nicewrap select{position:absolute;inset:0;width:100%;height:100%;opacity:0;pointer-events:none}
.nicesel{
  display:flex;align-items:center;justify-content:space-between;gap:10px;width:100%;
  padding:9px 11px;border:1px solid var(--line);border-radius:7px;background:var(--surface);
  box-shadow:var(--shadow);font-size:13px;color:var(--ink);cursor:pointer;text-align:left;
}
:root[data-theme="dark"] .nicesel{background:var(--surface-2)}
.nicesel:hover{border-color:#c8cdd6}
.nicesel svg{width:16px;height:16px;color:var(--muted);flex:0 0 auto;transition:transform .15s}
.nicewrap.open .nicesel{border-color:var(--brand);box-shadow:0 0 0 3px var(--brand-soft)}
.nicewrap.open .nicesel svg{transform:rotate(180deg)}
.niceopts{
  position:absolute;left:0;right:0;top:calc(100% + 5px);z-index:40;padding:5px;
  background:var(--surface);border:1px solid var(--line);border-radius:9px;
  box-shadow:0 14px 34px rgba(16,24,40,.16);display:none;max-height:280px;overflow:auto;
}
.nicewrap.open .niceopts{display:block}
.niceopts div{
  display:flex;align-items:center;justify-content:space-between;gap:10px;
  padding:8px 10px;border-radius:6px;font-size:13px;color:var(--ink-2);cursor:pointer;
}
.niceopts div:hover,.niceopts div.active{background:var(--surface-2)}
.niceopts div[aria-selected="true"]{color:var(--brand-ink);font-weight:650}
.niceopts div[aria-selected="true"]:after{content:"";width:14px;height:8px;border-left:2px solid currentColor;border-bottom:2px solid currentColor;transform:rotate(-45deg) translateY(-3px);flex:0 0 auto}
input[type=text],input[type=password],input[type=email],input[type=number],input[type=file],select,textarea{
  width:100%;padding:9px 11px;border:1px solid var(--line);border-radius:7px;
  background:var(--surface);box-shadow:var(--shadow);outline:none;font-size:13px;
}
:root[data-theme="dark"] input,:root[data-theme="dark"] select,:root[data-theme="dark"] textarea{background:var(--surface-2)}
input:focus,select:focus,textarea:focus{border-color:var(--brand);box-shadow:0 0 0 3px var(--brand-soft)}
input[readonly]{color:var(--muted)}
.input-group{display:flex;gap:0}
.input-group input{border-top-right-radius:0;border-bottom-right-radius:0}
.input-group .btn{border-top-left-radius:0;border-bottom-left-radius:0;border-left:0;white-space:nowrap}
.input-suffix{display:flex;align-items:center;gap:9px}
.input-suffix input{flex:1}
.input-suffix > .input-group{flex:1;min-width:0}
.input-suffix > .codebox{flex:1;min-width:0}
.unit{color:var(--muted);font-size:12px;white-space:nowrap}
.form-actions{display:flex;gap:9px;flex-wrap:wrap;margin-top:18px}

/* ---------- buttons ---------- */
.btn{
  display:inline-flex;align-items:center;justify-content:center;gap:7px;
  padding:8px 13px;border-radius:7px;border:1px solid var(--line);
  background:var(--surface);color:var(--ink-2);font-size:12.5px;font-weight:600;
  text-decoration:none;cursor:pointer;box-shadow:var(--shadow);white-space:nowrap;
}
.btn svg{width:15px;height:15px}
.btn:hover{background:var(--surface-2)}
.btn.primary{background:var(--brand);border-color:var(--brand);color:#fff}
.btn.primary:hover{background:var(--brand-strong);border-color:var(--brand-strong)}
.btn.accent{color:var(--brand-ink);border-color:var(--brand)}
.btn.accent:hover{background:var(--brand-soft)}
.btn.danger{color:var(--bad);border-color:var(--bad)}
.btn.danger:hover{background:var(--bad-soft)}
.btn.small{padding:5px 9px;font-size:11.5px}
.btn.icon{padding:6px;width:30px}
.btn.icon svg{width:16px;height:16px}
.btn[disabled]{opacity:.55;cursor:not-allowed}
.linky{background:none;border:0;box-shadow:none;color:var(--brand-ink);padding:0;font-size:12.5px;font-weight:600;cursor:pointer;text-decoration:none}
.linky:hover{text-decoration:underline;background:none}

/* ---------- pills ---------- */
.pill{
  display:inline-flex;align-items:center;gap:5px;padding:3px 10px;border-radius:999px;
  font-size:11.5px;font-weight:700;background:var(--surface-2);color:var(--ink-2);white-space:nowrap;
}
.pill.ok{background:var(--ok-soft);color:var(--ok)}
.pill.warn{background:var(--warn-soft);color:var(--warn)}
.pill.bad{background:var(--bad-soft);color:var(--bad)}
.pill.info{background:var(--info-soft);color:var(--info)}
.pill.brand{background:var(--brand-soft);color:var(--brand-ink)}
.page-head .pill{font-size:12px;padding:4px 11px}

/* ---------- tables ---------- */
.table-wrap{overflow-x:auto}
table{width:100%;border-collapse:collapse;font-size:12.5px}
th{
  text-align:left;font-weight:600;color:var(--muted);font-size:11.5px;
  padding:11px var(--pad);border-bottom:1px solid var(--line);white-space:nowrap;background:var(--surface-2);
}
td{padding:12px var(--pad);border-bottom:1px solid var(--line-soft);vertical-align:middle;color:var(--ink)}
tr:last-child td{border-bottom:0}
td.num,th.num{text-align:right;font-variant-numeric:tabular-nums}
td.when{color:var(--muted);white-space:nowrap;font-variant-numeric:tabular-nums}
td.msg{color:var(--ink);min-width:220px}
td.stage{white-space:nowrap}
.stage-cell{display:flex;align-items:center;gap:9px;font-weight:600;color:var(--ink);white-space:nowrap}
.stage-cell svg{width:15px;height:15px;color:var(--muted);flex:0 0 auto}
td.msg{font-weight:500}
td.who{color:var(--ink-2);white-space:nowrap;font-variant-numeric:tabular-nums}
.table-foot{display:flex;justify-content:space-between;align-items:center;gap:14px;padding:12px var(--pad);border-top:1px solid var(--line);color:var(--muted);font-size:12px;flex-wrap:wrap}
.pager{display:flex;gap:5px;align-items:center}
.pager button{min-width:29px;height:29px;border-radius:7px;border:1px solid var(--line);background:var(--surface);color:var(--ink-2);font-size:12px;cursor:pointer;padding:0 7px}
.pager button[aria-current="true"]{background:var(--brand);border-color:var(--brand);color:#fff;font-weight:650}
.pager button[disabled]{opacity:.4;cursor:not-allowed}
.empty{padding:44px 20px;text-align:center;color:var(--muted)}
.empty b{display:block;color:var(--ink);font-size:15px;margin-bottom:5px}

/* ---------- toggles ---------- */
.toggle{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;padding:14px 0;border-top:1px solid var(--line-soft)}
.toggle:first-child{border-top:0;padding-top:0}
.toggle .label{font-size:13px;font-weight:580;color:var(--ink-2)}
.toggle .label span{display:block;font-weight:400;color:var(--muted);font-size:12px;margin-top:2px}
.switch{position:relative;flex:0 0 auto;width:40px;height:22px}
.switch input{position:absolute;opacity:0;width:100%;height:100%;margin:0;cursor:pointer;z-index:2}
.switch .slider{position:absolute;inset:0;border-radius:999px;background:#d0d5dd;transition:background .15s}
.switch .slider:before{content:"";position:absolute;top:3px;left:3px;width:16px;height:16px;border-radius:50%;background:#fff;transition:transform .15s;box-shadow:0 1px 2px rgba(16,24,40,.2)}
.switch input:checked + .slider{background:var(--brand)}
.switch input:checked + .slider:before{transform:translateX(18px)}
.switch input:focus-visible + .slider{box-shadow:0 0 0 3px var(--brand-soft)}

/* ---------- misc ---------- */
.banner{display:flex;align-items:center;gap:10px;padding:12px 15px;border-radius:var(--radius);font-size:13px;font-weight:560;margin-bottom:18px;border:1px solid transparent}
.banner.ok{background:var(--ok-soft);color:var(--ok);border-color:var(--ok-soft)}
.banner.warn{background:var(--warn-soft);color:var(--warn)}
.banner.bad{background:var(--bad-soft);color:var(--bad)}
.banner svg{width:18px;height:18px;flex:0 0 auto}
.banner.update{background:var(--brand-soft);color:var(--brand-ink);border-color:transparent;align-items:center}
.banner.update span{flex:1}
.banner.update b{color:var(--ink)}
:root[data-theme="dark"] .banner.update b{color:var(--ink)}
.banner form{margin:0}
.callout{background:var(--surface-2);border:1px solid var(--line);border-radius:8px;padding:13px 15px;color:var(--muted);font-size:12px}
.mono{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:12px}
.codebox{background:var(--surface-2);border:1px solid var(--line);border-radius:8px;padding:13px;color:var(--ink-2);font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:11.5px;line-height:1.75;overflow:auto;white-space:pre-wrap;word-break:break-word}
.codebox.bad{background:var(--bad-soft);border-color:var(--bad-soft);color:var(--bad)}
details.disclosure{border-top:1px solid var(--line);margin-top:16px;padding-top:0}
details.disclosure summary{
  cursor:pointer;padding:14px 0 0;font-size:13px;font-weight:580;color:var(--ink-2);
  display:flex;align-items:center;justify-content:space-between;list-style:none;
}
details.disclosure summary::-webkit-details-marker{display:none}
details.disclosure summary:after{content:"";width:8px;height:8px;border-right:1.6px solid var(--muted);border-bottom:1.6px solid var(--muted);transform:rotate(45deg);margin-right:4px}
details.disclosure[open] summary:after{transform:rotate(-135deg)}
details.disclosure .disclosure-body{padding:4px 0 2px}
.filters{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-bottom:16px}
.filters select{width:auto;min-width:150px}
.filters .nicewrap{min-width:172px}
.filters .nicewrap:last-of-type{margin-left:auto}
.filters .search{flex:1;min-width:200px;position:relative}
.filters .search svg{position:absolute;left:11px;top:50%;transform:translateY(-50%);width:16px;height:16px;color:var(--muted)}
.filters .search input{padding-left:34px}
.menu{position:relative}
.menu-panel{
  position:absolute;right:0;top:calc(100% + 6px);z-index:30;min-width:210px;padding:6px;
  background:var(--surface);border:1px solid var(--line);border-radius:9px;
  box-shadow:0 12px 30px rgba(16,24,40,.14);display:none;
}
.menu[open] .menu-panel,.menu-panel.open{display:block}
.menu-panel a,.menu-panel button{
  display:flex;width:100%;align-items:center;gap:9px;padding:8px 10px;border:0;border-radius:6px;
  background:none;color:var(--ink-2);font-size:12.5px;font-weight:540;text-align:left;text-decoration:none;cursor:pointer;
}
.menu-panel a:hover,.menu-panel button:hover{background:var(--surface-2)}
.menu-panel .destructive{color:var(--bad)}
.hidden{display:none!important}
.alerts{position:fixed;right:22px;bottom:22px;z-index:60;display:flex;flex-direction:column;gap:9px;max-width:380px}
.alert{
  display:flex;gap:10px;align-items:flex-start;padding:12px 14px;border-radius:10px;
  background:var(--surface);border:1px solid var(--line);box-shadow:0 12px 30px rgba(16,24,40,.16);font-size:12.5px;
}
.alert b{display:block;color:var(--bad);font-size:12.5px}
.alert span{color:var(--muted);display:block;margin-top:2px}
.modal{position:fixed;inset:0;z-index:70;background:rgba(16,24,40,.45);display:grid;place-items:center;padding:24px}
.modal[hidden]{display:none}
.modal-card{width:min(560px,100%);max-height:80vh;display:flex;flex-direction:column;background:var(--surface);border-radius:12px;border:1px solid var(--line);overflow:hidden}
.modal-head{padding:16px var(--pad);border-bottom:1px solid var(--line)}
.modal-head h2{font-size:15px}
.modal-head p{margin:4px 0 0;color:var(--muted);font-size:12px;overflow-wrap:anywhere}
.modal-list{overflow:auto;flex:1;padding:8px}
.modal-list button{
  display:flex;width:100%;align-items:center;gap:9px;padding:9px 11px;border:0;border-radius:7px;
  background:none;color:var(--ink-2);font-size:12.5px;text-align:left;cursor:pointer;
}
.modal-list button:hover{background:var(--surface-2)}
.modal-list svg{width:16px;height:16px;color:var(--muted);flex:0 0 auto}
.modal-foot{display:flex;justify-content:flex-end;gap:9px;padding:14px var(--pad);border-top:1px solid var(--line)}

@media(max-width:1100px){.content{padding:26px 22px 40px}}
@media(max-width:900px){
  .app{grid-template-columns:1fr}
  .sidebar{position:static;height:auto;flex-direction:row;flex-wrap:wrap;align-items:center;gap:8px}
  .brand{padding:0 12px 0 4px}
  .nav{flex-direction:row;flex-wrap:wrap}
  .nav-divider{display:none}
  .side-status{margin:0 0 0 auto}
  .page-head{flex-direction:column}
}
`

// uiJS drives only presentation: filtering and paging the activity table,
// confirming destructive posts, the folder picker, and in-window alerts. Every
// state change still travels through a normal authenticated form post.
const uiJS = `
(function(){
  "use strict";

  function esc(v){return String(v==null?"":v).replace(/[&<>"']/g,function(c){
    return {"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[c];});}

  function relTime(iso){
    var t=new Date(iso).getTime();
    if(isNaN(t))return "";
    var s=Math.round((Date.now()-t)/1000);
    if(s<45)return "just now";
    if(s<90)return "1m ago";
    if(s<3600)return Math.round(s/60)+"m ago";
    if(s<86400)return Math.round(s/3600)+"h ago";
    return Math.round(s/86400)+"d ago";
  }

  function duration(ms){
    if(!ms)return "–";
    if(ms<1000)return ms+"ms";
    if(ms<60000)return (ms/1000).toFixed(1)+"s";
    var m=Math.floor(ms/60000);
    return m+"m "+Math.round((ms%60000)/1000)+"s";
  }

  var STAGE_ICONS={
    gmail:"mail",reply:"send",agent:"agent",bridge:"bridge",
    host:"server",startup:"power",security:"shield"
  };
  function icon(name){
    var el=document.querySelector('#icon-'+name);
    return el?el.innerHTML:"";
  }
  function brandMark(name,size){
    var el=document.querySelector('#brand-'+name);
    if(!el)return "";
    return '<span class="bmark '+size+' '+name+'">'+el.innerHTML+'</span>';
  }

  function agentName(a){
    if(a==="C")return "Codex";
    if(a==="A")return "Claude";
    return a||"–";
  }

  // describe turns one logged event into the words a person reads: what the
  // step was, whose mark belongs next to it, and what happened.
  function describe(e){
    var agent=agentName(e.agent);
    var started=/started|queued|accepted for/i.test(e.message||"");
    var out={label:"",mark:"",status:"",tone:""};
    switch(e.stage){
      case "gmail":
        out.label=e.level==="info"?"Incoming SMS":"Gmail";
        out.mark=brandMark("google","sm");
        break;
      case "reply":
        out.label=(e.agent?agent+" reply":"Reply");
        out.mark=brandMark("voice","sm");
        break;
      case "agent":
        out.label=started?("To "+agent):(agent+" command");
        out.mark=e.agent==="A"?brandMark("claude","sm"):brandMark("codex","sm");
        break;
      case "bridge": out.label="Bridge"; break;
      case "host": out.label="Host"; break;
      case "startup": out.label="Startup"; break;
      case "security": out.label="Security"; break;
      default: out.label=e.stage||"Event";
    }
    if(!out.mark)out.mark='<span class="stage-glyph">'+icon(STAGE_ICONS[e.stage]||"bridge")+"</span>";

    if(e.level==="error"){out.status="Failed";out.tone="bad";}
    else if(e.level==="warn"){out.status="Attention";out.tone="warn";}
    else if(e.level==="success"){out.status=e.stage==="reply"?"Sent":"Completed";out.tone="ok";}
    else if(e.stage==="agent"&&started){out.status="Delivered";out.tone="warn";}
    else if(e.stage==="gmail"){out.status="Received";out.tone="info";}
    else {out.status="Noted";out.tone="info";}
    return out;
  }

  function formatPhone(n){
    var d=String(n).replace(/\D/g,"");
    if(d.length===10)return "+1 ("+d.slice(0,3)+") "+d.slice(3,6)+"-"+d.slice(6);
    return n;
  }

  /* ---------------- activity feeds ---------------- */

  var feeds=[];

  function rowHTML(e,columns){
    var when=new Date(e.time);
    var d=describe(e);
    var cells='<td class="when">'+esc(isNaN(when)?e.time:when.toLocaleString())+'</td>'+
      '<td class="stage"><span class="stage-cell">'+d.mark+esc(d.label)+'</span></td>'+
      '<td><span class="pill '+d.tone+'">'+esc(d.status)+'</span></td>'+
      '<td class="msg">'+esc(e.message)+'</td>';
    if(columns!=="compact"){
      cells+='<td class="who">'+esc(e.sender?formatPhone(e.sender):"–")+'</td>'+
        '<td class="who">'+esc(agentName(e.agent))+'</td>'+
        '<td class="num">'+esc(duration(e.durationMs))+'</td>';
    }
    return '<tr>'+cells+'</tr>';
  }

  function matches(e,f){
    if(f.stage&&e.stage!==f.stage)return false;
    if(f.agent&&e.agent!==f.agent)return false;
    if(f.since&&new Date(e.time).getTime()<f.since)return false;
    if(f.q){
      var hay=(e.message+" "+(e.sender||"")+" "+(e.stage||"")).toLowerCase();
      if(hay.indexOf(f.q)<0)return false;
    }
    return true;
  }

  function readFilters(root){
    var f={stage:"",agent:"",q:"",since:0};
    var stage=root.querySelector("[data-filter=stage]");
    var agent=root.querySelector("[data-filter=agent]");
    var q=root.querySelector("[data-filter=q]");
    var range=root.querySelector("[data-filter=range]");
    if(stage)f.stage=stage.value;
    if(agent)f.agent=agent.value;
    if(q)f.q=q.value.trim().toLowerCase();
    if(range&&range.value){
      var hours=parseFloat(range.value);
      if(hours>0)f.since=Date.now()-hours*3600*1000;
    }
    return f;
  }

  function renderFeed(feed){
    var body=feed.root.querySelector("[data-events]");
    if(!body)return;
    var all=feed.events||[];
    var list=feed.filterable?all.filter(function(e){return matches(e,readFilters(feed.root));}):all;
    var perPage=feed.perPage||10;
    var pages=Math.max(1,Math.ceil(list.length/perPage));
    if(feed.page>pages)feed.page=pages;
    var start=(feed.page-1)*perPage;
    var slice=list.slice(start,start+perPage);

    if(!slice.length){
      body.innerHTML='<tr><td colspan="7"><div class="empty"><b>'+
        (all.length?"Nothing matches these filters":"No activity yet")+'</b>'+
        (all.length?"Change the filters to see more events.":"Text your Google Voice number to see the first event here.")+
        '</div></td></tr>';
    } else {
      body.innerHTML=slice.map(function(e){return rowHTML(e,feed.columns);}).join("");
    }

    var count=feed.root.querySelector("[data-count]");
    if(count){
      count.textContent=list.length
        ? (feed.filterable
            ? "Showing "+(start+1)+" to "+Math.min(start+perPage,list.length)+" of "+list.length+" events"
            : "Showing "+slice.length+" of "+all.length+" events")
        : "No events recorded";
    }
    renderPager(feed,pages);
    updateSummaries(feed,all);
  }

  function renderPager(feed,pages){
    var pager=feed.root.querySelector("[data-pager]");
    if(!pager)return;
    if(pages<2){pager.innerHTML="";return;}
    var html='<button data-page="'+(feed.page-1)+'"'+(feed.page<=1?" disabled":"")+' aria-label="Previous page">‹</button>';
    var shown=[];
    for(var i=1;i<=pages;i++){
      if(i<=2||i>pages-1||Math.abs(i-feed.page)<=1)shown.push(i);
    }
    var last=0;
    shown.forEach(function(i){
      if(last&&i-last>1)html+='<button disabled>…</button>';
      html+='<button data-page="'+i+'"'+(i===feed.page?' aria-current="true"':"")+'>'+i+'</button>';
      last=i;
    });
    html+='<button data-page="'+(feed.page+1)+'"'+(feed.page>=pages?" disabled":"")+' aria-label="Next page">›</button>';
    pager.innerHTML=html;
  }

  function updateSummaries(feed,events){
    feed.root.querySelectorAll("[data-summary]").forEach(function(el){
      var stage=el.getAttribute("data-summary");
      var e=events.find(function(x){return x.stage===stage;});
      if(el.hasAttribute("data-summary-time")){
        el.textContent=e?relTime(e.time):"–";
        return;
      }
      if(!e){el.textContent="Waiting";el.className=el.className.replace(/\b(ok|warn|bad)\b/g,"");return;}
      var label={success:"OK",info:"Active",warn:"Attention",error:"Error"}[e.level]||"Active";
      var tone={success:"ok",info:"",warn:"warn",error:"bad"}[e.level]||"";
      el.textContent=label;
      el.className=el.className.replace(/\b(ok|warn|bad)\b/g,"").trim()+(tone?" "+tone:"");
    });
    var latest=feed.root.querySelector("[data-latest]");
    if(latest&&events.length)latest.textContent=describe(events[0]).label;
    var latestTime=feed.root.querySelector("[data-latest-time]");
    if(latestTime&&events.length)latestTime.textContent=relTime(events[0].time);
  }

  var lastErrorSeen=null;

  function noticeErrors(events){
    if(!document.body.dataset.alerts)return;
    var newest=events.find(function(e){return e.level==="error";});
    if(!newest)return;
    if(lastErrorSeen===null){lastErrorSeen=newest.time;return;}
    if(newest.time===lastErrorSeen)return;
    lastErrorSeen=newest.time;
    showAlert(describe(newest).label+" failed",newest.message);
  }

  function showAlert(title,detail){
    var host=document.querySelector(".alerts");
    if(!host){host=document.createElement("div");host.className="alerts";document.body.appendChild(host);}
    var el=document.createElement("div");
    el.className="alert";
    el.innerHTML='<span class="check bad">'+icon("alert")+'</span><span><b>'+esc(title)+'</b><span>'+esc(detail)+'</span></span>';
    host.appendChild(el);
    if(document.body.dataset.alertSound)beep();
    setTimeout(function(){el.remove();},9000);
  }

  function beep(){
    try{
      var Ctx=window.AudioContext||window.webkitAudioContext;
      if(!Ctx)return;
      var ctx=new Ctx(),osc=ctx.createOscillator(),gain=ctx.createGain();
      osc.frequency.value=660;gain.gain.value=.05;
      osc.connect(gain);gain.connect(ctx.destination);
      osc.start();osc.stop(ctx.currentTime+.14);
      setTimeout(function(){ctx.close();},400);
    }catch(_){}
  }

  function loadFeeds(){
    if(!feeds.length)return;
    fetch("/activity.json",{cache:"no-store"}).then(function(r){
      if(!r.ok)throw new Error("HTTP "+r.status);
      return r.json();
    }).then(function(events){
      events=events||[];
      noticeErrors(events);
      feeds.forEach(function(feed){feed.events=events;renderFeed(feed);});
    }).catch(function(err){
      feeds.forEach(function(feed){
        var body=feed.root.querySelector("[data-events]");
        if(body&&!feed.events)body.innerHTML='<tr><td colspan="7"><div class="empty"><b>Activity unavailable</b>'+esc(err.message)+'</div></td></tr>';
      });
    });
  }

  function initFeeds(){
    document.querySelectorAll("[data-feed]").forEach(function(root){
      var feed={
        root:root,page:1,events:null,
        perPage:parseInt(root.getAttribute("data-per-page"),10)||10,
        filterable:root.hasAttribute("data-filterable"),
        columns:root.getAttribute("data-columns")||"full"
      };
      feeds.push(feed);
      root.addEventListener("change",function(e){
        if(e.target.matches("[data-filter]")){feed.page=1;renderFeed(feed);}
      });
      root.addEventListener("input",function(e){
        if(e.target.matches("[data-filter=q]")){feed.page=1;renderFeed(feed);}
      });
      root.addEventListener("click",function(e){
        var btn=e.target.closest("[data-page]");
        if(!btn||btn.disabled)return;
        feed.page=parseInt(btn.getAttribute("data-page"),10)||1;
        renderFeed(feed);
      });
    });
    if(feeds.length){loadFeeds();setInterval(loadFeeds,4000);}
  }

  /* ---------------- live status ---------------- */

  var offline=false;

  function setOffline(down){
    if(down===offline)return;
    offline=down;
    var el=document.querySelector(".reconnect");
    if(down){
      if(!el){
        el=document.createElement("div");
        el.className="reconnect";
        el.innerHTML=icon("refresh")+"<span>Reconnecting to FlipAi…</span>";
        document.body.appendChild(el);
      }
      return;
    }
    if(el)el.remove();
  }

  function refreshStatus(){
    fetch("/status.json",{cache:"no-store"}).then(function(r){
      if(!r.ok)throw new Error("HTTP "+r.status);
      return r.json();
    }).then(function(s){
      // The host restarts itself after a settings change. Reload once it is
      // back so the window is never left showing a half-applied state.
      if(offline){setOffline(false);location.reload();return;}
      document.querySelectorAll("[data-status]").forEach(function(el){
        var key=el.getAttribute("data-status");
        if(!(key in s))return;
        var v=s[key];
        el.textContent=typeof v==="boolean"?(v?"Yes":"No"):String(v);
      });
    }).catch(function(){setOffline(true);});
  }

  /* ---------------- confirmations, menus, folder picker ---------------- */

  function initConfirms(){
    document.addEventListener("submit",function(e){
      var msg=e.target.getAttribute("data-confirm");
      if(msg&&!window.confirm(msg))e.preventDefault();
    });
  }

  function initAutoSubmit(){
    document.addEventListener("change",function(e){
      var el=e.target.closest("[data-autosubmit]");
      if(el&&el.form)el.form.submit();
    });
  }

  function initMenus(){
    document.addEventListener("click",function(e){
      var trigger=e.target.closest("[data-menu-trigger]");
      var open=document.querySelectorAll(".menu-panel.open");
      open.forEach(function(p){
        if(!trigger||p!==trigger.parentElement.querySelector(".menu-panel"))p.classList.remove("open");
      });
      if(trigger){
        e.preventDefault();
        var panel=trigger.parentElement.querySelector(".menu-panel");
        if(panel)panel.classList.toggle("open");
      }
    });
  }

  var pickerTarget=null,pickerPath="";

  function initPicker(){
    var modal=document.querySelector("#folder-picker");
    if(!modal)return;
    document.addEventListener("click",function(e){
      var open=e.target.closest("[data-browse]");
      if(open){
        e.preventDefault();
        pickerTarget=document.querySelector(open.getAttribute("data-browse"));
        loadFolders(pickerTarget?pickerTarget.value:"");
        modal.hidden=false;
        return;
      }
      if(e.target.closest("[data-picker-cancel]")||e.target===modal){modal.hidden=true;return;}
      if(e.target.closest("[data-picker-choose]")){
        if(pickerTarget&&pickerPath)pickerTarget.value=pickerPath;
        modal.hidden=true;
        return;
      }
      var entry=e.target.closest("[data-folder]");
      if(entry)loadFolders(entry.getAttribute("data-folder"));
    });
  }

  function loadFolders(path){
    var modal=document.querySelector("#folder-picker");
    var list=modal.querySelector(".modal-list");
    var current=modal.querySelector("[data-picker-path]");
    list.innerHTML='<div class="empty">Loading…</div>';
    fetch("/folders.json?path="+encodeURIComponent(path||""),{cache:"no-store"})
      .then(function(r){return r.json();})
      .then(function(data){
        pickerPath=data.path||"";
        current.textContent=pickerPath||"This PC";
        var html="";
        if(data.parent!==undefined&&data.parent!==null&&data.parent!==data.path){
          html+='<button type="button" data-folder="'+esc(data.parent)+'">'+icon("folder-up")+"Up one level</button>";
        }
        (data.folders||[]).forEach(function(f){
          html+='<button type="button" data-folder="'+esc(f.path)+'">'+icon("folder")+esc(f.name)+"</button>";
        });
        if(!(data.folders||[]).length)html+='<div class="empty">No sub-folders here.</div>';
        if(data.error)html='<div class="empty"><b>Cannot open that folder</b>'+esc(data.error)+"</div>"+html;
        list.innerHTML=html;
      })
      .catch(function(err){list.innerHTML='<div class="empty"><b>Folder list unavailable</b>'+esc(err.message)+"</div>";});
  }

  function initCopy(){
    document.addEventListener("click",function(e){
      var btn=e.target.closest("[data-copy]");
      if(!btn)return;
      e.preventDefault();
      var src=document.querySelector(btn.getAttribute("data-copy"));
      if(!src)return;
      var text=src.textContent||"";
      if(navigator.clipboard)navigator.clipboard.writeText(text).catch(function(){});
      var old=btn.getAttribute("title");
      btn.setAttribute("title","Copied");
      setTimeout(function(){btn.setAttribute("title",old||"Copy");},1500);
    });
  }

  function initReveal(){
    document.addEventListener("click",function(e){
      var btn=e.target.closest("[data-reveal]");
      if(!btn)return;
      e.preventDefault();
      var box=document.querySelector(btn.getAttribute("data-reveal"));
      if(!box)return;
      box.classList.toggle("hidden");
      if(!box.classList.contains("hidden")){
        var first=box.querySelector("input,select,textarea");
        if(first)first.focus();
      }
    });
  }


  /* ---------------- select ---------------- */

  // Windows draws a native select popup that cannot be styled, so each select
  // gets a matching listbox. The real <select> stays in the form and keeps
  // working (and keeps its value) if this script never runs.
  function initSelects(){
    document.querySelectorAll("select").forEach(function(sel){
      if(sel.multiple||sel.dataset.plain!==undefined||sel.closest(".nicewrap"))return;
      var wrap=document.createElement("div");
      wrap.className="nicewrap";
      sel.parentNode.insertBefore(wrap,sel);
      wrap.appendChild(sel);

      var button=document.createElement("button");
      button.type="button";
      button.className="nicesel";
      button.setAttribute("aria-haspopup","listbox");
      button.setAttribute("aria-expanded","false");
      if(sel.id)button.setAttribute("aria-labelledby",sel.id+"-label");
      var text=document.createElement("span");
      var caret=document.createElement("span");
      caret.innerHTML=icon("chevron-down");
      button.appendChild(text);
      button.appendChild(caret);
      wrap.appendChild(button);

      var list=document.createElement("div");
      list.className="niceopts";
      list.setAttribute("role","listbox");
      wrap.appendChild(list);

      function paint(){
        text.textContent=sel.options[sel.selectedIndex]?sel.options[sel.selectedIndex].text:"";
        list.innerHTML="";
        Array.prototype.forEach.call(sel.options,function(opt,i){
          var row=document.createElement("div");
          row.setAttribute("role","option");
          row.setAttribute("aria-selected",i===sel.selectedIndex?"true":"false");
          row.textContent=opt.text;
          row.addEventListener("click",function(){choose(i);});
          list.appendChild(row);
        });
      }
      function open(){
        closeAll();
        wrap.classList.add("open");
        button.setAttribute("aria-expanded","true");
      }
      function close(){
        wrap.classList.remove("open");
        button.setAttribute("aria-expanded","false");
      }
      function choose(i){
        if(i<0||i>=sel.options.length)return;
        if(i!==sel.selectedIndex){
          sel.selectedIndex=i;
          sel.dispatchEvent(new Event("change",{bubbles:true}));
        }
        paint();
        close();
        button.focus();
      }
      button.addEventListener("click",function(e){
        e.preventDefault();
        wrap.classList.contains("open")?close():open();
      });
      button.addEventListener("keydown",function(e){
        if(e.key==="ArrowDown"||e.key==="ArrowUp"){
          e.preventDefault();
          if(!wrap.classList.contains("open")){open();return;}
          choose(sel.selectedIndex+(e.key==="ArrowDown"?1:-1));
        } else if(e.key==="Enter"||e.key===" "){
          e.preventDefault();
          wrap.classList.contains("open")?close():open();
        } else if(e.key==="Escape"){close();}
      });
      sel.addEventListener("change",paint);
      paint();
    });
    document.addEventListener("click",function(e){
      if(!e.target.closest(".nicewrap"))closeAll();
    });
  }

  function closeAll(){
    document.querySelectorAll(".nicewrap.open").forEach(function(w){
      w.classList.remove("open");
      var b=w.querySelector(".nicesel");
      if(b)b.setAttribute("aria-expanded","false");
    });
  }

  document.addEventListener("DOMContentLoaded",function(){
    initSelects();
    initFeeds();
    initConfirms();
    initAutoSubmit();
    initMenus();
    initPicker();
    initCopy();
    initReveal();
    refreshStatus();
    setInterval(refreshStatus,3000);
    document.querySelectorAll("[data-rel-time]").forEach(function(el){
      var iso=el.getAttribute("data-rel-time");
      if(iso)el.textContent=relTime(iso);
    });
  });
})();
`

func (a *App) serveAsset(w http.ResponseWriter, r *http.Request) {
	var body, mime string
	switch {
	case strings.HasSuffix(r.URL.Path, ".css"):
		body, mime = uiCSS, "text/css; charset=utf-8"
	case strings.HasSuffix(r.URL.Path, ".js"):
		body, mime = uiJS, "text/javascript; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	// Assets change only with the build, and the page requests them with a
	// version query, so they can be cached for the life of the window.
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(body))
}
