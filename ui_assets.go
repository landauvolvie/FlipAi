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
/* ===========================================================================
   FlipAi design tokens

   One scale for space, one for radius, one for elevation. Everything below is
   built from these, so a control cannot end up a slightly different shape or a
   slightly different grey from the control beside it.
   =========================================================================== */
:root{
  --bg:#f7f8fa;
  --bg-side:#ffffff;
  --surface:#ffffff;
  --surface-2:#f4f5f8;
  --surface-3:#eceef3;
  --ink:#14161c;
  --ink-2:#3d4351;
  --muted:#6b7280;
  --faint:#9aa1ae;
  --line:#e5e7ec;
  --line-soft:#eef0f4;
  --brand:#5c53f0;
  --brand-strong:#4b41e0;
  --brand-ink:#4b41e0;
  --brand-soft:#f0eefe;
  --brand-line:#d8d3fd;
  --ok:#0a7c50; --ok-soft:#e7f8f0; --ok-line:#bce8d5;
  --warn:#a4600a; --warn-soft:#fdf4e3; --warn-line:#f3ddb0;
  --bad:#b3251c; --bad-soft:#fdefee; --bad-line:#f6cdc9;
  --info:#1358c9; --info-soft:#ebf3ff; --info-line:#c6dcfb;

  --shadow-xs:0 1px 2px rgba(16,24,40,.04);
  --shadow-sm:0 1px 2px rgba(16,24,40,.05),0 1px 3px rgba(16,24,40,.04);
  --shadow-md:0 2px 4px rgba(16,24,40,.04),0 8px 20px -6px rgba(16,24,40,.09);
  --shadow-lg:0 12px 34px -10px rgba(16,24,40,.20),0 4px 10px rgba(16,24,40,.05);
  --ring:0 0 0 3px rgba(92,83,240,.18);

  --r-xs:6px; --r-sm:9px; --r-md:12px; --r-lg:16px; --r-xl:20px;
  --radius:var(--r-lg);
  --radius-control:10px;
  --pad:20px;
  --gap:16px;
  --control-h:38px;
  --sidebar-w:236px;
  --ease:cubic-bezier(.32,.72,0,1);
}
:root[data-theme="dark"]{
  --bg:#0f1014;
  --bg-side:#131419;
  --surface:#181a20;
  --surface-2:#1f2128;
  --surface-3:#272a33;
  --ink:#f2f3f6;
  --ink-2:#cfd3dc;
  --muted:#949aa6;
  --faint:#6f7683;
  --line:#282b34;
  --line-soft:#22242b;
  --brand:#9a90ff;
  --brand-strong:#b1a8ff;
  --brand-ink:#b6adff;
  --brand-soft:#211f3d;
  --brand-line:#332f5c;
  --ok:#4fd6a0; --ok-soft:#0e2a20; --ok-line:#1c4436;
  --warn:#f0b45f; --warn-soft:#2c2314; --warn-line:#4a3a1c;
  --bad:#fb9086; --bad-soft:#2f1917; --bad-line:#4d2723;
  --info:#84b4f7; --info-soft:#12233a; --info-line:#1f3a5c;
  --shadow-xs:none;
  --shadow-sm:0 1px 2px rgba(0,0,0,.35);
  --shadow-md:0 2px 6px rgba(0,0,0,.35);
  --shadow-lg:0 16px 40px -12px rgba(0,0,0,.62);
  --ring:0 0 0 3px rgba(154,144,255,.22);
}
@media(prefers-color-scheme:dark){
  :root:not([data-theme="light"]):not([data-theme="dark"]){
    --bg:#0f1014; --bg-side:#131419; --surface:#181a20; --surface-2:#1f2128; --surface-3:#272a33;
    --ink:#f2f3f6; --ink-2:#cfd3dc; --muted:#949aa6; --faint:#6f7683;
    --line:#282b34; --line-soft:#22242b;
    --brand:#9a90ff; --brand-strong:#b1a8ff; --brand-ink:#b6adff; --brand-soft:#211f3d; --brand-line:#332f5c;
    --ok:#4fd6a0; --ok-soft:#0e2a20; --ok-line:#1c4436;
    --warn:#f0b45f; --warn-soft:#2c2314; --warn-line:#4a3a1c;
    --bad:#fb9086; --bad-soft:#2f1917; --bad-line:#4d2723;
    --info:#84b4f7; --info-soft:#12233a; --info-line:#1f3a5c;
    --shadow-xs:none;
    --shadow-sm:0 1px 2px rgba(0,0,0,.35);
    --shadow-md:0 2px 6px rgba(0,0,0,.35);
    --shadow-lg:0 16px 40px -12px rgba(0,0,0,.62);
    --ring:0 0 0 3px rgba(154,144,255,.22);
  }
}
/* Compact tightens the scale rather than only the padding, so the whole window
   shrinks proportionally instead of leaving oversized cards full of gaps. */
:root[data-compact="1"]{--pad:14px;--gap:11px;--radius:13px;--r-lg:13px;--radius-control:9px;--control-h:34px;--sidebar-w:208px}

*{box-sizing:border-box}
html,body{height:100%}
body{
  margin:0;background:var(--bg);color:var(--ink);
  font:14px/1.55 "Segoe UI Variable Text","Segoe UI",Inter,system-ui,-apple-system,"Helvetica Neue",sans-serif;
  -webkit-font-smoothing:antialiased;text-rendering:optimizeLegibility;
  font-variant-numeric:tabular-nums;
}
button,input,select,textarea{font:inherit;color:inherit}
a{color:var(--brand-ink);text-underline-offset:2px}
h1,h2,h3{margin:0;letter-spacing:-.015em;line-height:1.25}
::selection{background:var(--brand-soft);color:var(--brand-ink)}
:focus-visible{outline:none;box-shadow:var(--ring);border-radius:var(--r-xs)}
::-webkit-scrollbar{width:11px;height:11px}
::-webkit-scrollbar-track{background:transparent}
::-webkit-scrollbar-thumb{background:var(--surface-3);border-radius:99px;border:3px solid transparent;background-clip:content-box}
::-webkit-scrollbar-thumb:hover{background:var(--faint);background-clip:content-box}
@media(prefers-reduced-motion:reduce){*,*:before,*:after{animation-duration:.001ms!important;transition-duration:.001ms!important}}

/* ---------- shell ---------- */
.app{display:grid;grid-template-columns:var(--sidebar-w) minmax(0,1fr);min-height:100vh}
.sidebar{
  background:var(--bg-side);border-right:1px solid var(--line);
  padding:16px 12px 14px;display:flex;flex-direction:column;gap:2px;
  position:sticky;top:0;height:100vh;
}
.brand{display:flex;align-items:center;gap:10px;padding:6px 8px 20px;font-size:16px;font-weight:660;letter-spacing:-.01em}
.brand-mark{
  width:30px;height:30px;border-radius:9px;flex:0 0 auto;
  background:linear-gradient(140deg,var(--brand) 0%,#8b7bff 100%);
  color:#fff;display:grid;place-items:center;font-weight:800;font-size:14px;
  box-shadow:0 3px 10px -2px rgba(92,83,240,.55);
}
.nav{display:flex;flex-direction:column;gap:1px}
.nav-label{
  padding:14px 12px 6px;font-size:10.5px;font-weight:700;letter-spacing:.07em;
  text-transform:uppercase;color:var(--faint);
}
.nav a{
  position:relative;display:flex;align-items:center;gap:10px;padding:8px 11px;
  border-radius:var(--radius-control);color:var(--ink-2);text-decoration:none;
  font-size:13.5px;font-weight:520;transition:background .14s var(--ease),color .14s var(--ease);
}
.nav a svg{width:17px;height:17px;flex:0 0 auto;opacity:.75;transition:opacity .14s}
.nav a:hover{background:var(--surface-2);color:var(--ink)}
.nav a:hover svg{opacity:1}
.nav a[aria-current="page"]{background:var(--brand-soft);color:var(--brand-ink);font-weight:650}
.nav a[aria-current="page"] svg{opacity:1}
.nav a[aria-current="page"]:before{
  content:"";position:absolute;left:-12px;top:50%;transform:translateY(-50%);
  width:3px;height:17px;border-radius:0 3px 3px 0;background:var(--brand);
}
.nav-divider{height:1px;background:var(--line);margin:10px 8px}
.side-status{
  margin-top:auto;border:1px solid var(--line);border-radius:var(--r-md);
  background:var(--surface);padding:11px 12px;font-size:11.5px;color:var(--muted);
  box-shadow:var(--shadow-xs);
}
.side-status b{display:flex;align-items:center;gap:7px;color:var(--ink);font-size:12.5px;font-weight:600}
.side-status span{display:block;margin-top:4px}
.side-update{
  display:flex;align-items:center;gap:6px;margin-top:6px;text-decoration:none;
  color:var(--brand-ink);font-weight:660;
}
.side-update:hover{text-decoration:underline}
.side-update svg{width:13px;height:13px;flex:0 0 auto}
.side-update span{margin-top:0}
.dot{
  width:8px;height:8px;border-radius:50%;background:#12b76a;flex:0 0 auto;
  box-shadow:0 0 0 3px rgba(18,183,106,.16);
}
.dot.paused{background:#f79009;box-shadow:0 0 0 3px rgba(247,144,9,.16)}
.dot.stopped{background:#f04438;box-shadow:0 0 0 3px rgba(240,68,56,.16)}

.content{padding:28px 32px 52px;min-width:0;max-width:1320px}
.page-head{display:flex;justify-content:space-between;align-items:flex-start;gap:20px;margin-bottom:22px}
.page-head h1{font-size:26px;font-weight:660;display:flex;align-items:center;gap:11px;flex-wrap:wrap}
.page-head p{margin:6px 0 0;color:var(--muted);font-size:13px;max-width:720px}
.page-actions{display:flex;gap:8px;flex-wrap:wrap;padding-top:3px}

/* ---------- stat tiles ---------- */
.tiles{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:var(--gap);margin-bottom:var(--gap)}
.tile{
  background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);
  padding:var(--pad);box-shadow:var(--shadow-sm);min-width:0;
  transition:box-shadow .18s var(--ease),border-color .18s var(--ease);
}
.tile:hover{box-shadow:var(--shadow-md);border-color:var(--line)}
.tile-top{display:flex;align-items:flex-start;justify-content:space-between;gap:10px}
.tile-icon{
  width:32px;height:32px;border-radius:var(--r-sm);background:var(--surface-2);
  border:1px solid var(--line);display:grid;place-items:center;color:var(--muted);flex:0 0 auto;
}
.tile-icon svg{width:17px;height:17px}
.tile-icon.brandish{background:var(--brand-soft);border-color:var(--brand-line);color:var(--brand-ink)}
.tile h3{font-size:12px;font-weight:600;margin-top:14px;line-height:1.35;color:var(--muted);letter-spacing:.01em}
.tile .val{margin-top:4px;font-size:15px;font-weight:640;line-height:1.35;overflow-wrap:anywhere;letter-spacing:-.01em}
.tile .sub{margin-top:4px;font-size:12px;color:var(--muted);overflow-wrap:anywhere}
.tile .val.ok{color:var(--ok)}
.tile .val.warn{color:var(--warn)}
.tile .val.bad{color:var(--bad)}
.tile .val.brand{color:var(--brand-ink)}
.tile .val.big{font-size:24px;font-weight:680;margin-top:6px;line-height:1.2}
b.ok{color:var(--ok)}b.warn{color:var(--warn)}b.bad{color:var(--bad)}
#icon-sprites{display:none}
.hint{margin:6px 0 0;font-size:11.5px;color:var(--muted);line-height:1.5}
.reconnect{
  position:fixed;left:50%;top:16px;transform:translateX(-50%);z-index:80;
  display:flex;align-items:center;gap:9px;padding:9px 16px;border-radius:999px;
  background:var(--warn-soft);color:var(--warn);border:1px solid var(--warn-line);
  font-size:12.5px;font-weight:620;box-shadow:var(--shadow-lg);
}
.reconnect svg{animation:spin 1.1s linear infinite}
@keyframes spin{to{transform:rotate(360deg)}}
.tile-foot{
  display:flex;justify-content:space-between;gap:10px;align-items:center;
  margin-top:14px;padding-top:11px;border-top:1px solid var(--line-soft);
  font-size:11.5px;color:var(--muted);
}
.check{color:var(--ok);display:grid;place-items:center}
.check svg{width:18px;height:18px}
.check.warn{color:var(--warn)}
.check.bad{color:var(--bad)}

/* ---------- cards ---------- */
.card{
  background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);
  box-shadow:var(--shadow-sm);margin-bottom:var(--gap);overflow:hidden;
}
.card-head{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;padding:var(--pad)}
.card-head + .card-body{padding-top:0}
.card-head.divided{border-bottom:1px solid var(--line-soft);background:var(--surface)}
.card-head.divided + .card-body{padding-top:var(--pad)}
.card-head h2{font-size:15px;font-weight:640;display:flex;align-items:center;gap:9px;flex-wrap:wrap}
.card-head p{margin:4px 0 0;color:var(--muted);font-size:12.5px;max-width:640px}
.card-head .head-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap}
.card-body{padding:var(--pad)}
.card-body > :first-child{margin-top:0}
.card-title-row{display:flex;align-items:center;gap:12px;min-width:0}
.card-title-row .mark{
  width:38px;height:38px;border-radius:var(--r-sm);display:grid;place-items:center;
  font-weight:800;font-size:14px;flex:0 0 auto;
}
.mark svg{width:19px;height:19px}
.mark.mail{background:var(--info-soft);color:var(--info)}
.mark.shield{background:var(--brand-soft);color:var(--brand-ink)}
.section-label{
  font-size:10.5px;font-weight:700;letter-spacing:.07em;text-transform:uppercase;
  color:var(--faint);margin:26px 0 10px;
}
.section-label:first-child{margin-top:0}

/* ---------- brand marks ---------- */
.bmark{
  width:32px;height:32px;border-radius:var(--r-sm);flex:0 0 auto;
  display:grid;place-items:center;overflow:hidden;
}
.bmark svg{width:19px;height:19px}
.bmark .glyph{font-weight:800;font-size:13px;line-height:1;letter-spacing:.5px}
.bmark.google{background:#fff;border:1px solid var(--line)}
:root[data-theme="dark"] .bmark.google{background:#f7f8fa}
.bmark.voice{background:#e8f0fe;border:1px solid transparent}
:root[data-theme="dark"] .bmark.voice{background:#1b2a44}
.bmark.codex{background:#0d1117;color:#fff}
.bmark.codex .glyph{font-family:ui-monospace,Consolas,monospace;font-size:12px}
.bmark.claude{background:#d97757;color:#fff}
.bmark.lg{width:40px;height:40px;border-radius:11px}
.bmark.lg svg{width:23px;height:23px}
.bmark.lg .glyph{font-size:15px}
.bmark.sm{width:22px;height:22px;border-radius:6px}
.bmark.sm svg{width:14px;height:14px}
.bmark.sm .glyph{font-size:9px}
.cards-2{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:var(--gap);align-items:start}
@media(max-width:1000px){.cards-2{grid-template-columns:minmax(0,1fr)}}
.cards-2 .card{margin-bottom:0}

/* ---------- state rows ---------- */
.rows{display:flex;flex-direction:column}
.row{display:flex;justify-content:space-between;align-items:center;gap:16px;padding:12px 0;border-top:1px solid var(--line-soft)}
.row:first-child{border-top:0;padding-top:0}
.row:last-child{padding-bottom:0}
.row .label{font-size:13px;font-weight:560;color:var(--ink-2);min-width:0}
.row .label span{display:block;font-weight:400;color:var(--muted);font-size:11.5px;margin-top:3px;line-height:1.5}
.row .value{display:flex;align-items:center;gap:9px;font-size:12.5px;color:var(--muted);text-align:right;overflow-wrap:anywhere;flex-wrap:wrap;justify-content:flex-end}
.row .value b{color:var(--ink);font-weight:580}
.row .value b.ok{color:var(--ok)}
.row .value b.warn{color:var(--warn)}
.row .value b.bad{color:var(--bad)}

/* ---------- forms ---------- */
.field{margin-top:var(--gap);min-width:0}
.field:first-child{margin-top:0}
.field label{display:block;font-size:12px;font-weight:600;color:var(--ink-2);margin-bottom:6px;letter-spacing:.005em}
.field .hint{margin:6px 0 0;font-size:11.5px;color:var(--muted)}
.grid-2{display:grid;grid-template-columns:repeat(auto-fit,minmax(250px,1fr));gap:var(--gap)}
.grid-3{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:var(--gap)}
select{
  appearance:none;-webkit-appearance:none;padding-right:34px;cursor:pointer;
  background-image:url("data:image/svg+xml;charset=utf-8,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 20 20' fill='none' stroke='%236b7280' stroke-width='1.8' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m5.5 8 4.5 4.5L14.5 8'/%3E%3C/svg%3E");
  background-repeat:no-repeat;background-position:right 10px center;background-size:16px;
}
.nicewrap{position:relative}
.nicewrap select{position:absolute;inset:0;width:100%;height:100%;opacity:0;pointer-events:none}
.nicesel{
  display:flex;align-items:center;justify-content:space-between;gap:10px;width:100%;
  min-height:var(--control-h);padding:8px 11px;border:1px solid var(--line);
  border-radius:var(--radius-control);background:var(--surface);
  box-shadow:var(--shadow-xs);font-size:13px;color:var(--ink);cursor:pointer;text-align:left;
  transition:border-color .14s,box-shadow .14s;
}
:root[data-theme="dark"] .nicesel{background:var(--surface-2)}
.nicesel:hover{border-color:var(--faint)}
.nicesel svg{width:16px;height:16px;color:var(--muted);flex:0 0 auto;transition:transform .16s var(--ease)}
.nicewrap.open .nicesel{border-color:var(--brand);box-shadow:var(--ring)}
.nicewrap.open .nicesel svg{transform:rotate(180deg)}
/* Fixed, not absolute. As an absolutely positioned child the list was clipped
   by any ancestor with overflow:hidden — .card is one — so a long menu such as
   the update interval lost its lower options entirely, and every menu could run
   off the bottom of the window with no way to reach the rest. Taking it out of
   the flow means no ancestor can crop it; openNiceOpts places it against the
   button's viewport rect and flips it above when there is more room there. */
.niceopts{
  position:fixed;z-index:80;padding:5px;
  background:var(--surface);border:1px solid var(--line);border-radius:var(--r-md);
  box-shadow:var(--shadow-lg);display:none;overflow:auto;
}
.nicewrap.open .niceopts{display:block;animation:pop .13s var(--ease)}
@keyframes pop{from{opacity:0;transform:translateY(-4px)}to{opacity:1;transform:none}}
.niceopts div{
  display:flex;align-items:center;justify-content:space-between;gap:10px;
  padding:8px 10px;border-radius:var(--r-xs);font-size:13px;color:var(--ink-2);cursor:pointer;
}
.niceopts div:hover,.niceopts div.active{background:var(--surface-2)}
.niceopts div[aria-selected="true"]{color:var(--brand-ink);font-weight:640}
.niceopts div[aria-selected="true"]:after{content:"";width:13px;height:7px;border-left:2px solid currentColor;border-bottom:2px solid currentColor;transform:rotate(-45deg) translateY(-3px);flex:0 0 auto}
input[type=text],input[type=password],input[type=email],input[type=number],input[type=search],input[type=file],select,textarea{
  width:100%;min-height:var(--control-h);padding:8px 11px;border:1px solid var(--line);
  border-radius:var(--radius-control);box-shadow:var(--shadow-xs);
  background:var(--surface);outline:none;font-size:13px;
  transition:border-color .14s,box-shadow .14s;
}
textarea{min-height:96px;line-height:1.6;resize:vertical;padding:11px 12px}
:root[data-theme="dark"] input,:root[data-theme="dark"] select,:root[data-theme="dark"] textarea{background:var(--surface-2)}
input:hover,textarea:hover{border-color:var(--faint)}
input:focus,select:focus,textarea:focus{border-color:var(--brand);box-shadow:var(--ring)}
input::placeholder,textarea::placeholder{color:var(--faint)}
/* Readonly fields carry long generated values such as the resume command,
   which previously ran past the edge of the box with no way to see the end. */
input[readonly]{color:var(--muted);text-overflow:ellipsis;background:var(--surface-2)}
.input-group{display:flex;gap:0}
.input-group input{border-top-right-radius:0;border-bottom-right-radius:0}
.input-group .btn{border-top-left-radius:0;border-bottom-left-radius:0;border-left:0;white-space:nowrap}
.input-suffix{display:flex;align-items:center;gap:9px}
.input-suffix input{flex:1}
.input-suffix > .input-group{flex:1;min-width:0}
.input-suffix > .codebox{flex:1;min-width:0}
.unit{color:var(--muted);font-size:12px;white-space:nowrap}
.form-actions{display:flex;gap:9px;flex-wrap:wrap;margin-top:18px;align-items:center}

/* ---------- buttons ---------- */
.btn{
  display:inline-flex;align-items:center;justify-content:center;gap:7px;
  min-height:var(--control-h);padding:7px 13px;border-radius:var(--radius-control);
  border:1px solid var(--line);background:var(--surface);color:var(--ink-2);
  font-size:12.5px;font-weight:600;text-decoration:none;cursor:pointer;
  box-shadow:var(--shadow-xs);white-space:nowrap;
  transition:background .14s var(--ease),border-color .14s var(--ease),transform .1s var(--ease),box-shadow .14s;
}
.btn svg{width:15px;height:15px}
.btn:hover{background:var(--surface-2);border-color:var(--faint)}
.btn:active{transform:translateY(.5px)}
.btn.primary{
  background:linear-gradient(180deg,#6a61f5,var(--brand));
  border-color:var(--brand-strong);color:#fff;box-shadow:0 1px 2px rgba(16,24,40,.10),0 3px 10px -4px rgba(92,83,240,.6);
}
.btn.primary:hover{background:linear-gradient(180deg,var(--brand),var(--brand-strong));border-color:var(--brand-strong)}
:root[data-theme="dark"] .btn.primary{color:#14121f;background:linear-gradient(180deg,#a89eff,var(--brand))}
.btn.accent{color:var(--brand-ink);border-color:var(--brand-line);background:var(--surface)}
.btn.accent:hover{background:var(--brand-soft);border-color:var(--brand)}
.btn.danger{color:var(--bad);border-color:var(--bad-line)}
.btn.danger:hover{background:var(--bad-soft);border-color:var(--bad)}
.btn.small{min-height:30px;padding:4px 10px;font-size:11.5px}
.btn.small svg{width:14px;height:14px}
.btn.icon{padding:0;width:var(--control-h);flex:0 0 auto}
.btn.icon svg{width:16px;height:16px}
.btn.block{width:100%}
.btn[disabled]{opacity:.5;cursor:not-allowed}
.btn[disabled]:hover{background:var(--surface);border-color:var(--line)}
.linky{
  display:inline-flex;align-items:center;gap:3px;
  background:none;border:0;box-shadow:none;color:var(--brand-ink);padding:0;min-height:0;
  font-size:12.5px;font-weight:620;cursor:pointer;text-decoration:none;white-space:nowrap;
}
.linky svg{width:13px;height:13px;flex:0 0 auto}
.linky:hover{text-decoration:underline;background:none;border-color:transparent}

/* ---------- pills ---------- */
.pill{
  display:inline-flex;align-items:center;gap:5px;padding:2px 9px;border-radius:999px;
  font-size:11px;font-weight:660;background:var(--surface-2);color:var(--muted);
  border:1px solid var(--line);white-space:nowrap;letter-spacing:.005em;
}
.pill.ok{background:var(--ok-soft);color:var(--ok);border-color:var(--ok-line)}
.pill.warn{background:var(--warn-soft);color:var(--warn);border-color:var(--warn-line)}
.pill.bad{background:var(--bad-soft);color:var(--bad);border-color:var(--bad-line)}
.pill.info{background:var(--info-soft);color:var(--info);border-color:var(--info-line)}
.pill.brand{background:var(--brand-soft);color:var(--brand-ink);border-color:var(--brand-line)}
.page-head .pill{font-size:11.5px;padding:3px 10px}

/* ---------- tables ---------- */
.table-wrap{overflow-x:auto}
table{width:100%;border-collapse:collapse;font-size:12.5px}
th{
  text-align:left;font-weight:620;color:var(--muted);font-size:11px;
  letter-spacing:.05em;text-transform:uppercase;
  padding:10px var(--pad);border-bottom:1px solid var(--line);white-space:nowrap;background:var(--surface-2);
}
td{padding:11px var(--pad);border-bottom:1px solid var(--line-soft);vertical-align:middle;color:var(--ink)}
tbody tr{transition:background .12s}
tbody tr:hover{background:var(--surface-2)}
tr:last-child td{border-bottom:0}
td.num,th.num{text-align:right;font-variant-numeric:tabular-nums}
td.when{color:var(--muted);white-space:nowrap;font-variant-numeric:tabular-nums}
td.msg{color:var(--ink);min-width:220px;font-weight:500}
td.stage{white-space:nowrap}
.stage-cell{display:flex;align-items:center;gap:9px;font-weight:600;color:var(--ink);white-space:nowrap}
.stage-cell svg{width:15px;height:15px;color:var(--muted);flex:0 0 auto}
td.who{color:var(--ink-2);white-space:nowrap;font-variant-numeric:tabular-nums}
.table-foot{
  display:flex;justify-content:space-between;align-items:center;gap:14px;
  padding:11px var(--pad);border-top:1px solid var(--line);color:var(--muted);
  font-size:12px;flex-wrap:wrap;background:var(--surface);
}
.pager{display:flex;gap:4px;align-items:center}
.pager button{
  min-width:28px;height:28px;border-radius:var(--r-sm);border:1px solid var(--line);
  background:var(--surface);color:var(--ink-2);font-size:12px;cursor:pointer;padding:0 7px;
}
.pager button:hover:not([disabled]){background:var(--surface-2)}
.pager button[aria-current="true"]{background:var(--brand);border-color:var(--brand);color:#fff;font-weight:650}
.pager button[disabled]{opacity:.4;cursor:not-allowed}
.empty{padding:46px 20px;text-align:center;color:var(--muted);font-size:12.5px}
.empty b{display:block;color:var(--ink);font-size:14.5px;margin-bottom:5px;font-weight:620}

/* ---------- toggles ----------
   A card often splits its toggles across several <form> elements, because each
   one posts to a different action. Keying "no rule above me" off :first-child
   alone therefore removed the divider from every one of them and ran them
   together; only a toggle that really is first inside the card body loses it. */
.toggle{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;padding:13px 0;border-top:1px solid var(--line-soft)}
.card-body > .toggle:first-child,
.card-body > form:first-child > .toggle:first-child,
.disclosure-body > .toggle:first-child,
.prompt-editor + .toggle{border-top:0;padding-top:0}
.toggle:last-child{padding-bottom:0}
.toggle .label{font-size:13px;font-weight:560;color:var(--ink-2)}
.toggle .label span{display:block;font-weight:400;color:var(--muted);font-size:11.5px;margin-top:3px;line-height:1.5}
.switch{position:relative;flex:0 0 auto;width:38px;height:21px;margin-top:1px}
.switch input{position:absolute;opacity:0;width:100%;height:100%;margin:0;cursor:pointer;z-index:2}
.switch .slider{position:absolute;inset:0;border-radius:999px;background:var(--surface-3);transition:background .18s var(--ease)}
.switch .slider:before{
  content:"";position:absolute;top:2.5px;left:2.5px;width:16px;height:16px;border-radius:50%;
  background:#fff;transition:transform .18s var(--ease);box-shadow:0 1px 3px rgba(16,24,40,.28);
}
.switch input:checked + .slider{background:var(--brand)}
.switch input:checked + .slider:before{transform:translateX(17px)}
.switch input:focus-visible + .slider{box-shadow:var(--ring)}

/* ---------- banners, callouts, code ---------- */
.banner{
  display:flex;align-items:center;gap:11px;padding:12px 15px;border-radius:var(--r-md);
  font-size:13px;font-weight:540;margin-bottom:18px;border:1px solid transparent;
}
.banner.ok{background:var(--ok-soft);color:var(--ok);border-color:var(--ok-line)}
.banner.warn{background:var(--warn-soft);color:var(--warn);border-color:var(--warn-line)}
.banner.bad{background:var(--bad-soft);color:var(--bad);border-color:var(--bad-line)}
.banner svg{width:18px;height:18px;flex:0 0 auto}
.banner.update{background:var(--brand-soft);color:var(--brand-ink);border-color:var(--brand-line)}
.banner.update span{flex:1}
.banner.update b{color:var(--ink)}
.banner form{margin:0}
.callout{
  background:var(--surface-2);border:1px solid var(--line);border-radius:var(--r-md);
  padding:12px 15px;color:var(--muted);font-size:12px;line-height:1.6;
}
.mono{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:12px}
code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:.92em;background:var(--surface-2);border:1px solid var(--line-soft);border-radius:5px;padding:1px 5px}
.codebox{
  background:var(--surface-2);border:1px solid var(--line);border-radius:var(--r-md);
  padding:12px 13px;color:var(--ink-2);font-family:ui-monospace,SFMono-Regular,Consolas,monospace;
  font-size:11.5px;line-height:1.75;overflow:auto;white-space:pre-wrap;word-break:break-word;
}
.codebox.bad{background:var(--bad-soft);border-color:var(--bad-line);color:var(--bad)}
details.disclosure{border-top:1px solid var(--line-soft);margin-top:16px}
details.disclosure summary{
  cursor:pointer;padding:14px 0 0;font-size:12.5px;font-weight:620;color:var(--ink-2);
  display:flex;align-items:center;justify-content:space-between;list-style:none;
}
details.disclosure summary::-webkit-details-marker{display:none}
details.disclosure summary:after{content:"";width:7px;height:7px;border-right:1.7px solid var(--muted);border-bottom:1.7px solid var(--muted);transform:rotate(45deg);margin-right:4px;transition:transform .16s var(--ease)}
details.disclosure[open] summary:after{transform:rotate(-135deg)}
details.disclosure .disclosure-body{padding:14px 0 2px}
.filters{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-bottom:var(--gap)}
.filters select{width:auto;min-width:150px}
.filters .nicewrap{min-width:168px}
.filters .nicewrap:last-of-type{margin-left:auto}
.filters .search{flex:1;min-width:200px;position:relative}
.filters .search svg{position:absolute;left:11px;top:50%;transform:translateY(-50%);width:16px;height:16px;color:var(--muted)}
.filters .search input{padding-left:34px}
.menu{position:relative}
.menu-panel{
  position:absolute;right:0;top:calc(100% + 6px);z-index:30;min-width:212px;padding:5px;
  background:var(--surface);border:1px solid var(--line);border-radius:var(--r-md);
  box-shadow:var(--shadow-lg);display:none;
}
.menu[open] .menu-panel,.menu-panel.open{display:block;animation:pop .13s var(--ease)}
.menu-panel a,.menu-panel button{
  display:flex;width:100%;align-items:center;gap:9px;padding:8px 10px;border:0;border-radius:var(--r-xs);
  background:none;color:var(--ink-2);font-size:12.5px;font-weight:540;text-align:left;text-decoration:none;cursor:pointer;
}
.menu-panel a:hover,.menu-panel button:hover{background:var(--surface-2)}
.menu-panel svg{width:15px;height:15px;color:var(--muted);flex:0 0 auto}
.menu-panel .destructive{color:var(--bad)}
.hidden{display:none!important}
.alerts{position:fixed;right:20px;bottom:20px;z-index:60;display:flex;flex-direction:column;gap:9px;max-width:380px}
.alert{
  display:flex;gap:10px;align-items:flex-start;padding:12px 14px;border-radius:var(--r-md);
  background:var(--surface);border:1px solid var(--line);box-shadow:var(--shadow-lg);font-size:12.5px;
  animation:pop .16s var(--ease);
}
.alert b{display:block;color:var(--bad);font-size:12.5px}
.alert span{color:var(--muted);display:block;margin-top:2px}
.modal{position:fixed;inset:0;z-index:70;background:rgba(16,24,40,.5);display:grid;place-items:center;padding:24px;backdrop-filter:blur(2px)}
.modal[hidden]{display:none}
.modal-card{
  width:min(560px,100%);max-height:80vh;display:flex;flex-direction:column;
  background:var(--surface);border-radius:var(--r-lg);border:1px solid var(--line);
  overflow:hidden;box-shadow:var(--shadow-lg);animation:pop .16s var(--ease);
}
.modal-head{padding:16px var(--pad);border-bottom:1px solid var(--line)}
.modal-head h2{font-size:15px}
.modal-head p{margin:4px 0 0;color:var(--muted);font-size:12px;overflow-wrap:anywhere}
.modal-list{overflow:auto;flex:1;padding:8px}
.modal-list button{
  display:flex;width:100%;align-items:center;gap:9px;padding:9px 11px;border:0;border-radius:var(--radius-control);
  background:none;color:var(--ink-2);font-size:12.5px;text-align:left;cursor:pointer;
}
.modal-list button:hover{background:var(--surface-2)}
.modal-list svg{width:16px;height:16px;color:var(--muted);flex:0 0 auto}
.modal-foot{display:flex;justify-content:flex-end;gap:9px;padding:14px var(--pad);border-top:1px solid var(--line)}

/* ---------- prompt editor ----------
   The SMS instruction each agent sends with every text. It is the one place in
   FlipAi where the user writes something the model reads, so it gets a real
   editor: a live character count, a reset to the shared wording, and a preview
   of the exact prompt the agent receives. */
.prompt-editor{border:1px solid var(--line);border-radius:var(--r-md);background:var(--surface-2);overflow:hidden}
.prompt-editor-head{
  display:flex;align-items:center;justify-content:space-between;gap:12px;
  padding:10px 12px;border-bottom:1px solid var(--line);background:var(--surface);flex-wrap:wrap;
}
.prompt-editor-head .label{font-size:12px;font-weight:620;color:var(--ink-2);display:flex;align-items:center;gap:8px;min-width:0}
.prompt-editor-head .label svg{width:15px;height:15px;flex:0 0 auto;color:var(--muted)}
.prompt-editor-head .tools{display:flex;align-items:center;gap:8px}
.prompt-editor textarea{
  border:0;border-radius:0;box-shadow:none;background:var(--surface);min-height:104px;
  font-size:13px;line-height:1.65;
}
.prompt-editor textarea:focus{box-shadow:inset 0 0 0 2px var(--brand-soft);border:0}
.prompt-editor-foot{
  display:flex;align-items:center;justify-content:space-between;gap:12px;
  padding:9px 12px;border-top:1px solid var(--line);font-size:11.5px;color:var(--muted);flex-wrap:wrap;
}
.prompt-count{font-variant-numeric:tabular-nums;font-weight:600}
.prompt-count.over{color:var(--bad)}
.prompt-preview{
  margin:0;padding:12px 13px;background:var(--surface-2);border-top:1px solid var(--line);
  font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:11px;line-height:1.7;
  color:var(--muted);white-space:pre-wrap;word-break:break-word;
}
.prompt-preview b{color:var(--ink-2);font-weight:600}
.prompt-preview .ph{color:var(--brand-ink);font-weight:600}

/* ---------- agents workbench ----------
   The Agents screen is a master/detail: one rail of agents, one pane of
   settings. The radio inputs that drive it are hidden, so selecting an agent
   needs no script and the page still works if the script never runs. */
.agents-shell{display:grid;grid-template-columns:264px minmax(0,1fr);gap:var(--gap);align-items:start}
.agent-switch{position:absolute;opacity:0;pointer-events:none}
.agent-rail{
  background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);
  padding:12px;box-shadow:var(--shadow-sm);position:sticky;top:28px;
}
.agent-rail .nav-label{padding:6px 8px 8px}
.agent-list{display:flex;flex-direction:column;gap:4px}
.agent-item{
  display:flex;align-items:center;gap:11px;padding:10px 11px;cursor:pointer;
  border:1px solid transparent;border-radius:var(--radius-control);min-width:0;
  transition:background .14s var(--ease),border-color .14s var(--ease);
}
.agent-item:hover{background:var(--surface-2)}
.agent-item-copy{min-width:0;flex:1}
.agent-item-copy b{display:flex;align-items:center;gap:7px;font-size:13px;font-weight:620;color:var(--ink)}
.agent-item-copy span{display:block;color:var(--muted);font-size:11px;margin-top:2px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.agent-chip{
  display:inline-flex;padding:1px 7px;border-radius:999px;font-size:10px;font-weight:680;
  background:var(--ok-soft);color:var(--ok);border:1px solid var(--ok-line);
}
.agent-chip.warn{background:var(--warn-soft);color:var(--warn);border-color:var(--warn-line)}
#agent-codex:checked~.agents-shell .agent-item[for="agent-codex"],
#agent-claude:checked~.agents-shell .agent-item[for="agent-claude"],
#agent-shared:checked~.agents-shell .agent-item[for="agent-shared"]{
  background:var(--brand-soft);border-color:var(--brand-line);
}
#agent-codex:checked~.agents-shell .agent-item[for="agent-codex"] b,
#agent-claude:checked~.agents-shell .agent-item[for="agent-claude"] b,
#agent-shared:checked~.agents-shell .agent-item[for="agent-shared"] b{color:var(--brand-ink)}
.agent-pane{display:none;min-width:0}
#agent-codex:checked~.agents-shell #codex-pane,
#agent-claude:checked~.agents-shell #claude-pane,
#agent-shared:checked~.agents-shell #shared-pane{display:block}
.agent-head{
  display:flex;align-items:center;justify-content:space-between;gap:16px;
  background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);
  padding:14px var(--pad);margin-bottom:var(--gap);box-shadow:var(--shadow-sm);flex-wrap:wrap;
}
.agent-head-main{display:flex;align-items:center;gap:12px;min-width:0}
.agent-head-main h2{font-size:19px;font-weight:660;display:flex;align-items:center;gap:9px;flex-wrap:wrap}
.agent-head-main p{margin:3px 0 0;color:var(--muted);font-size:12px}
.agent-head-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap;justify-content:flex-end}
.agent-stat{min-width:0}
.agent-stat label{display:block;font-size:11px;color:var(--muted);margin-bottom:5px;font-weight:500}
.agent-stat b{font-size:12.5px;font-weight:600;color:var(--ink)}
.agent-stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:var(--gap)}
.agent-id{font:11.5px ui-monospace,Consolas,monospace;color:var(--ink-2);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.agent-none{color:var(--faint);font-size:12px}
.inline-actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:14px}
@media(max-width:1080px){
  .agents-shell{grid-template-columns:1fr}
  .agent-rail{position:static}
  /* The rail becomes a row of cards, so a group heading has to claim the whole
     line instead of sitting in a cell beside the agent it labels. */
  .agent-list{display:grid;grid-template-columns:repeat(auto-fit,minmax(190px,1fr))}
  .agent-list .nav-label{grid-column:1/-1;padding:8px 2px 0}
}

@media(max-width:1100px){.content{padding:24px 20px 40px}}
@media(max-width:900px){
  .app{grid-template-columns:1fr}
  .sidebar{position:static;height:auto;flex-direction:row;flex-wrap:wrap;align-items:center;gap:8px}
  .brand{padding:0 12px 0 4px}
  .nav{flex-direction:row;flex-wrap:wrap}
  .nav-divider,.nav-label{display:none}
  .nav a[aria-current="page"]:before{display:none}
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


  /* ---------------- SMS instruction editors ---------------- */

  // Each editor shows what the agent will actually receive: the fenced command
  // FlipAi builds around the text message, then the instruction below it. An
  // empty box is not an empty instruction — it falls back to the shared
  // wording, and the preview says so by showing that wording.
  function initPromptEditors(){
    document.querySelectorAll("[data-prompt-editor]").forEach(function(box){
      var input=box.querySelector("[data-prompt-input]");
      if(!input)return;
      var count=box.querySelector("[data-prompt-count]");
      var preview=box.querySelector("[data-prompt-preview]");
      var reset=box.querySelector("[data-prompt-reset]");
      var fallback=input.getAttribute("data-prompt-fallback")||"";
      var max=parseInt(input.getAttribute("maxlength"),10)||2000;

      function paint(){
        var typed=input.value;
        var used=typed.length;
        if(count){
          count.textContent=used+" / "+max;
          if(used>max-40){count.classList.add("over");}else{count.classList.remove("over");}
        }
        if(preview){
          var effective=typed.replace(/^\s+|\s+$/g,"")||fallback;
          preview.innerHTML="<b>&lt;sms_command&gt;</b>\n"+
            '<span class="ph">your text message</span>\n'+
            "<b>&lt;/sms_command&gt;</b>\n\n"+esc(effective);
        }
      }
      input.addEventListener("input",paint);
      if(reset){
        reset.addEventListener("click",function(e){
          e.preventDefault();
          input.value="";
          paint();
          input.focus();
        });
      }
      paint();
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
      // place puts the fixed-position list against the button and decides which
      // way it opens. Below is preferred; it flips above when there is more room
      // there, and the height is always capped to the space actually available
      // so the last option can be reached by scrolling instead of being lost off
      // the bottom of the window.
      function place(){
        var r=button.getBoundingClientRect();
        var margin=8, gap=5;
        var below=innerHeight-r.bottom-margin, above=r.top-margin;
        var up=below<180&&above>below;
        var room=Math.max(120,Math.floor(up?above-gap:below-gap));
        list.style.left=Math.round(r.left)+"px";
        list.style.width=Math.round(r.width)+"px";
        list.style.maxHeight=Math.min(320,room)+"px";
        if(up){
          list.style.top="auto";
          list.style.bottom=Math.round(innerHeight-r.top+gap)+"px";
        }else{
          list.style.bottom="auto";
          list.style.top=Math.round(r.bottom+gap)+"px";
        }
      }
      function open(){
        closeAll();
        wrap.classList.add("open");
        button.setAttribute("aria-expanded","true");
        place();
        // A fixed list does not travel with its button, so follow it while open.
        addEventListener("scroll",place,true);
        addEventListener("resize",place);
      }
      function close(){
        wrap.classList.remove("open");
        button.setAttribute("aria-expanded","false");
        removeEventListener("scroll",place,true);
        removeEventListener("resize",place);
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
    initPromptEditors();
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
