package panel

// داشبورد فارسی RTL — تعبیه‌شده در باینری پنل (بدون وابستگی خارجی، آفلاین)
const dashboardHTML = `<!DOCTYPE html>
<html lang="fa" dir="rtl">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>PhoenixTunnel Panel</title>
<style>
:root{
  --bg:#0a0710;--bg2:#120c1a;--card:#17101f;--card2:#1e1528;--line:#2c2038;
  --txt:#f4eefb;--dim:#a597b5;--fire:#ff6a1a;--fire2:#ff2d55;--gold:#ffc145;
  --ok:#2ee59d;--bad:#ff4d6d;--grad:linear-gradient(135deg,#ffc145 0%,#ff6a1a 45%,#ff2d55 100%);
}
*{box-sizing:border-box;margin:0;padding:0}
body{background:radial-gradient(1200px 600px at 85% -10%,#3a1420 0%,transparent 60%),radial-gradient(900px 500px at -10% 110%,#24103a 0%,transparent 60%),var(--bg);
  color:var(--txt);font-family:Vazirmatn,Tahoma,"Segoe UI",sans-serif;min-height:100vh;font-size:14px}
a{color:var(--gold)}
header{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 24px;border-bottom:1px solid var(--line);
  background:rgba(18,12,26,.8);backdrop-filter:blur(10px);position:sticky;top:0;z-index:5}
.brand{display:flex;align-items:center;gap:10px;font-weight:800;font-size:17px}
.logo{width:34px;height:34px;border-radius:10px;background:var(--grad);display:grid;place-items:center;box-shadow:0 0 24px #ff6a1a66;font-size:19px}
.brand small{display:block;font-weight:400;color:var(--dim);font-size:11px}
nav{display:flex;gap:6px;flex-wrap:wrap}
nav button{background:transparent;border:1px solid transparent;color:var(--dim);padding:8px 14px;border-radius:10px;cursor:pointer;font:inherit}
nav button.act{color:var(--txt);border-color:var(--line);background:var(--card2)}
.wrap{max-width:1180px;margin:22px auto;padding:0 16px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:14px}
.card{background:linear-gradient(180deg,var(--card2),var(--card));border:1px solid var(--line);border-radius:16px;padding:18px}
.card h3{font-size:13px;color:var(--dim);font-weight:500;margin-bottom:10px;display:flex;align-items:center;gap:8px}
.big{font-size:26px;font-weight:800}
.fire{background:var(--grad);-webkit-background-clip:text;background-clip:text;color:transparent}
.dot{display:inline-block;width:9px;height:9px;border-radius:50%;margin-left:6px;vertical-align:middle}
.on{background:var(--ok);box-shadow:0 0 10px var(--ok)}.off{background:var(--bad)}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{padding:11px 8px;border-bottom:1px solid var(--line);text-align:right;vertical-align:middle}
th{color:var(--dim);font-weight:500}
input,select,textarea{background:#0e0915;border:1px solid var(--line);color:var(--txt);border-radius:10px;padding:10px 12px;font:inherit;width:100%}
input:focus,textarea:focus{outline:none;border-color:var(--fire)}
button.b{background:var(--grad);border:none;color:#1a0b06;font-weight:800;cursor:pointer;border-radius:10px;padding:10px 16px;font:inherit;font-weight:800}
button.g{background:transparent;border:1px solid var(--line);color:var(--txt);cursor:pointer;border-radius:10px;padding:8px 12px;font:inherit}
button.g:hover{border-color:var(--fire)}
button.d{background:transparent;border:1px solid #5a2030;color:var(--bad);cursor:pointer;border-radius:10px;padding:8px 12px;font:inherit}
.row{display:flex;gap:10px;flex-wrap:wrap;align-items:center}
.row>*{flex:1;min-width:140px}
.row>button{flex:0 0 auto}
#login{max-width:400px;margin:12vh auto}
.err{color:var(--bad);font-size:12px;margin-top:8px;min-height:16px}
.hidden{display:none!important}
.mono{font-family:ui-monospace,Consolas,monospace;direction:ltr;unicode-bidi:embed}
.tag{font-size:11px;background:#2a1d36;border:1px solid var(--line);border-radius:20px;padding:3px 10px;margin-left:4px;display:inline-block;margin-bottom:3px}
.link{word-break:break-all;background:#0e0915;border:1px dashed #5a3a20;border-radius:12px;padding:12px;font-size:12px;color:#ffd9b8}
.linkbox{display:grid;grid-template-columns:200px 1fr;gap:18px;align-items:center}
.qr{width:200px;height:200px;border-radius:14px;background:#fff;padding:8px}
@media(max-width:700px){.linkbox{grid-template-columns:1fr}.qr{margin:auto}header{flex-direction:column}}
.mt{margin-top:14px}.muted{color:var(--dim);font-size:12px;line-height:1.9}
.toast{position:fixed;bottom:22px;left:50%;transform:translateX(-50%);background:var(--card2);border:1px solid var(--fire);padding:10px 18px;border-radius:12px;z-index:20}
.modal{position:fixed;inset:0;background:#000a;display:grid;place-items:center;z-index:10;padding:16px}
.modal .card{max-width:640px;width:100%;max-height:90vh;overflow:auto}
code.cmd{display:block;direction:ltr;text-align:left;background:#0e0915;border:1px solid var(--line);border-radius:10px;padding:10px;font-size:12px;white-space:pre-wrap;word-break:break-all}
.bar{height:6px;background:#2a1d36;border-radius:6px;overflow:hidden;margin-top:8px}.bar i{display:block;height:100%;background:var(--grad)}
</style>
</head>
<body>
<header>
  <div class="brand"><div class="logo">🔥</div><div>PhoenixTunnel<small>پنل کنترل هسته — ضد DPI</small></div></div>
  <nav id="nav" class="hidden">
    <button data-t="local" class="act">این سرور</button>
    <button data-t="nodes">نودها</button>
    <button data-t="settings">تنظیمات</button>
    <button onclick="logout()">خروج</button>
  </nav>
</header>

<div id="login" class="card">
  <h3>ورود به پنل</h3>
  <input id="u" placeholder="نام کاربری" autocomplete="username"><div class="mt"></div>
  <input id="p" type="password" placeholder="رمز عبور" autocomplete="current-password">
  <div class="err" id="lerr"></div>
  <button class="b" style="width:100%" onclick="doLogin()">ورود</button>
</div>

<div id="app" class="wrap hidden">
  <!-- این سرور -->
  <section id="t-local">
    <div class="grid" id="kpi"></div>
    <div class="card mt">
      <h3>🔗 لینک اتصال اپ اندروید</h3>
      <div id="localLink" class="muted">در حال بارگذاری…</div>
    </div>
    <div class="card mt">
      <h3>⚙️ موتورهای فعال</h3>
      <table><thead><tr><th>موتور</th><th>پورت</th><th>اتصال فعال</th><th>دانلود</th><th>آپلود</th><th>خطا</th></tr></thead>
      <tbody id="engines"></tbody></table>
    </div>
  </section>

  <!-- نودها -->
  <section id="t-nodes" class="hidden">
    <div class="card">
      <div class="row" style="justify-content:space-between">
        <h3 style="margin:0">🌍 نودهای دیگر (با ایجنت)</h3>
        <button class="g" onclick="toggle('newnode')">+ افزودن نود</button>
      </div>
      <div id="newnode" class="hidden mt">
        <div class="row">
          <input id="nn_name" placeholder="نام نود (مثلاً آلمان ۱)">
          <input id="nn_loc" placeholder="مکان (DE/NL/...)">
          <input id="nn_ip" placeholder="IP یا دامنه‌ی عمومی" class="mono">
          <button class="b" onclick="createNode()">ساخت</button>
        </div>
      </div>
      <table class="mt">
        <thead><tr><th>نام</th><th>مکان</th><th>آدرس</th><th>وضعیت</th><th>موتورها</th><th></th></tr></thead>
        <tbody id="nodes"></tbody>
      </table>
      <p class="muted mt">هر نود با <span class="mono">phoenix-agent</span> به این پنل وصل می‌شود، پروفایل (کلید + موتورها) را از اینجا می‌گیرد و آمار می‌فرستد. روی «اتصال» بزنید تا لینک اپ و دستور نصب ایجنت را ببینید.</p>
    </div>
  </section>

  <!-- تنظیمات -->
  <section id="t-settings" class="hidden">
    <div class="grid" style="grid-template-columns:repeat(auto-fill,minmax(320px,1fr))">
      <div class="card">
        <h3>🔑 تغییر رمز</h3>
        <input id="pw_old" type="password" placeholder="رمز فعلی"><div class="mt"></div>
        <input id="pw_new" type="password" placeholder="رمز جدید (حداقل ۶ کاراکتر)"><div class="mt"></div>
        <button class="b" onclick="changePw()">ذخیره</button>
      </div>
      <div class="card">
        <h3>👤 افزودن کاربر پنل</h3>
        <input id="nu_name" placeholder="نام کاربری"><div class="mt"></div>
        <input id="nu_pass" type="password" placeholder="رمز"><div class="mt"></div>
        <select id="nu_role"><option value="operator">اپراتور</option><option value="admin">ادمین</option><option value="viewer">بیننده</option></select>
        <div class="mt"></div><button class="b" onclick="addUser()">افزودن</button>
      </div>
      <div class="card">
        <h3>ℹ️ درباره</h3>
        <div class="muted" id="about"></div>
      </div>
    </div>
  </section>
</div>

<div id="modal" class="modal hidden" onclick="if(event.target===this)closeModal()"><div class="card" id="modalBody"></div></div>

<script>
let T = localStorage.getItem('phx_t') || ''; let PIN='';
const $ = id => document.getElementById(id);
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
function toast(m){const d=document.createElement('div');d.className='toast';d.textContent=m;document.body.appendChild(d);setTimeout(()=>d.remove(),2200)}
function fmtBytes(n){n=+n||0;const u=['B','KB','MB','GB','TB'];let i=0;while(n>=1024&&i<4){n/=1024;i++}return '<bdi dir="ltr">'+n.toFixed(i?1:0)+' '+u[i]+'</bdi>'}
function fmtDur(s){s=+s||0;const d=Math.floor(s/86400),h=Math.floor(s%86400/3600),m=Math.floor(s%3600/60);return (d?d+' روز ':'')+(h?h+' ساعت ':'')+m+' دقیقه'}
function port(l){const i=String(l||'').lastIndexOf(':');return i>=0?l.slice(i+1):l}
async function api(m, p, b){
  const r = await fetch(p, {method:m, headers:{'Authorization':'Bearer '+T,'Content-Type':'application/json'}, body:b?JSON.stringify(b):undefined});
  if(r.status===401){logout(); throw new Error('401')}
  if(r.status===204) return {};
  const j = await r.json().catch(()=>({}));
  if(!r.ok) throw new Error(j.error||('HTTP '+r.status));
  return j;
}
async function doLogin(){
  lerr.textContent='';
  const r = await fetch('/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({username:$('u').value,password:$('p').value})});
  const j = await r.json().catch(()=>({}));
  if(j.token){T=j.token; localStorage.setItem('phx_t',T); boot();}
  else lerr.textContent='نام کاربری یا رمز اشتباه است';
}
$('p').addEventListener('keydown',e=>{if(e.key==='Enter')doLogin()});
function logout(){ if(T) fetch('/api/v1/auth/logout',{method:'POST',headers:{'Authorization':'Bearer '+T}}).catch(()=>{}); localStorage.removeItem('phx_t'); T=''; location.reload(); }
function toggle(id){$(id).classList.toggle('hidden')}
let tab='local';
document.querySelectorAll('nav button[data-t]').forEach(b=>b.onclick=()=>{tab=b.dataset.t;
  document.querySelectorAll('nav button[data-t]').forEach(x=>x.classList.toggle('act',x===b));
  ['local','nodes','settings'].forEach(t=>$('t-'+t).classList.toggle('hidden',t!==tab)); refresh();});
async function boot(){
  $('login').classList.add('hidden'); $('app').classList.remove('hidden'); $('nav').classList.remove('hidden');
  try{const me=await api('GET','/api/v1/auth/me'); PIN=me.tls_pin||''; $('about').innerHTML='کاربر: <b>'+esc(me.username)+'</b> ('+esc(me.role)+')<br>نسخه: <span class="mono">'+esc(me.version||'dev')+'</span><br>پنل فقط روی HTTPS یا تونل SSH در دسترس قرار گیرد.';}catch(e){}
  refresh();
}
function copy(t){navigator.clipboard?.writeText(t).then(()=>toast('کپی شد ✓'),()=>prompt('کپی کنید:',t))}
function linkBlock(link, qrURL, name){
  return '<div class="linkbox"><img class="qr" alt="QR" src="'+qrURL+(qrURL.includes('?')?'&':'?')+'token='+encodeURIComponent(T)+'&t='+Date.now()+'">'
   +'<div><div class="link mono" id="lk">'+esc(link)+'</div><div class="row mt">'
   +'<button class="b" onclick="copy(document.getElementById(\'lk\').textContent)">📋 کپی لینک</button></div>'
   +'<p class="muted mt">در اپ PhoenixTunnel روی «افزودن» بزنید و لینک را Paste کنید یا QR را اسکن کنید. سرور <b>'+esc(name||'')+'</b> با همه‌ی تنظیمات (آدرس، پورت، کلید، SNI) اضافه می‌شود.</p></div></div>';
}
async function refresh(){
  if(!T) return;
  try{
    if(tab==='local') await refreshLocal();
    if(tab==='nodes') await refreshNodes();
  }catch(e){ if(e.message!=='401') console.warn(e) }
}
async function refreshLocal(){
  const l = await api('GET','/api/v1/local');
  if(!l.enabled){ $('kpi').innerHTML=''; $('localLink').innerHTML='<span class="muted">'+esc(l.error)+'</span>'; $('engines').innerHTML=''; return; }
  const st=l.stats||{}; const es=st.engines||[];
  let up=0,down=0,conns=0; es.forEach(e=>{up+=e.bytes_up;down+=e.bytes_down;conns+=e.conns_active});
  $('kpi').innerHTML =
    card('وضعیت هسته', '<span class="dot '+(l.online?'on':'off')+'"></span>'+(l.online?'آنلاین':'آفلاین'))
   +card('سرور', esc(l.name)+'<div class="muted mono">'+esc(l.host||'—')+'</div>')
   +card('اتصال‌های فعال', '<span class="fire">'+conns+'</span>')
   +card('ترافیک کل', '<span style="font-size:18px">⬇ '+fmtBytes(up)+' &nbsp; ⬆ '+fmtBytes(down)+'</span>')
   +card('زمان کارکرد', '<span style="font-size:16px">'+fmtDur(st.uptime_sec)+'</span>');
  $('localLink').innerHTML = l.link ? linkBlock(l.link,'/api/v1/local/qr.png',l.name) : '<span class="err">'+esc(l.link_error||'')+'</span>';
  const byName={}; es.forEach(e=>byName[e.name]=e);
  $('engines').innerHTML=(l.engines||[]).filter(e=>e.enabled).map(e=>{const s=byName[e.name]||{};
    return '<tr><td><span class="tag">'+esc(e.name)+'</span></td><td class="mono">'+esc(port(e.listen))+'</td><td>'+(s.conns_active||0)+'</td><td>'+fmtBytes(s.bytes_down)+'</td><td>'+fmtBytes(s.bytes_up)+'</td><td>'+(s.failures||0)+'</td></tr>'}).join('');
}
function card(t,v){return '<div class="card"><h3>'+t+'</h3><div class="big">'+v+'</div></div>'}
async function refreshNodes(){
  const nodes = await api('GET','/api/v1/nodes');
  $('nodes').innerHTML = nodes.length? nodes.map(n=>'<tr><td><b>'+esc(n.name)+'</b></td><td>'+esc(n.location||'—')+'</td>'
    +'<td class="mono">'+esc(n.ip||'—')+'</td>'
    +'<td><span class="dot '+(n.online?'on':'off')+'"></span>'+(n.online?'آنلاین':'آفلاین')+'</td>'
    +'<td>'+(n.engines||[]).filter(e=>e.enabled).map(e=>'<span class="tag">'+esc(e.name)+':'+esc(port(e.listen))+'</span>').join('')+'</td>'
    +'<td><div class="row" style="flex-wrap:nowrap"><button class="g" onclick="showNode(\''+n.id+'\')">اتصال</button>'
    +'<button class="g" onclick="switchEngine(\''+n.id+'\')">موتور</button>'
    +'<button class="d" onclick="delNode(\''+n.id+'\')">حذف</button></div></td></tr>').join('')
    : '<tr><td colspan="6" class="muted">هنوز نودی اضافه نشده.</td></tr>';
}
async function createNode(){
  try{ await api('POST','/api/v1/nodes',{name:$('nn_name').value,location:$('nn_loc').value,ip:$('nn_ip').value});
    $('newnode').classList.add('hidden'); toast('نود ساخته شد'); refresh(); }catch(e){toast(e.message)}
}
async function delNode(id){ if(!confirm('این نود حذف شود؟'))return; await api('DELETE','/api/v1/nodes/'+id); refresh(); }
async function switchEngine(id){
  const e = prompt('موتور جدید (chameleon / phoenix-udp / reality / phantom):','chameleon');
  if(e){ await api('POST','/api/v1/nodes/'+id+'/switch',{engine:e}); toast('در صف ارسال به ایجنت'); }
}
async function showNode(id){
  const n = await api('GET','/api/v1/nodes/'+id);
  let lk=''; try{ const l=await api('GET','/api/v1/nodes/'+id+'/link'); lk=linkBlock(l.link,'/api/v1/nodes/'+id+'/qr.png',n.name);}catch(e){lk='<div class="err">'+esc(e.message)+'</div>'}
  const panelURL = location.origin;
  $('modalBody').innerHTML='<h3>🔗 '+esc(n.name)+'</h3>'+lk
   +'<h3 class="mt">📦 نصب ایجنت روی سرور این نود</h3><code class="cmd">bash &lt;(curl -fsSL https://github.com/kankiturk8770/phonix/releases/latest/download/install.sh) --agent \\\n  --panel '+esc(panelURL)+' --token '+esc(n.agent_token)+' --node '+esc(n.id)+(PIN?' \\\n  --pin '+esc(PIN):'')+'</code>'
   +'<div class="row mt"><input id="ed_ip" class="mono" placeholder="IP/دامنه" value="'+esc(n.ip)+'"><button class="g" onclick="saveIP(\''+n.id+'\')">ذخیره آدرس</button>'
   +'<button class="d" onclick="rotate(\''+n.id+'\')">کلید جدید</button><button class="g" onclick="closeModal()">بستن</button></div>'
   +'<p class="muted mt">«کلید جدید» همه‌ی کلاینت‌های قبلی این نود را قطع می‌کند؛ لینک تازه را دوباره بدهید.</p>';
  $('modal').classList.remove('hidden');
}
async function saveIP(id){ await api('PUT','/api/v1/nodes/'+id,{ip:$('ed_ip').value}); toast('ذخیره شد'); showNode(id); refresh(); }
async function rotate(id){ if(!confirm('کلید عوض شود؟ کلاینت‌های قدیمی قطع می‌شوند.'))return; await api('POST','/api/v1/nodes/'+id+'/rotate-key'); showNode(id); }
function closeModal(){$('modal').classList.add('hidden')}
async function changePw(){ try{ await api('POST','/api/v1/auth/password',{old:$('pw_old').value,new:$('pw_new').value}); toast('رمز تغییر کرد'); $('pw_old').value=$('pw_new').value=''; }catch(e){toast(e.message)} }
async function addUser(){ try{ await api('POST','/api/v1/users',{username:$('nu_name').value,password:$('nu_pass').value,role:$('nu_role').value}); toast('کاربر اضافه شد'); }catch(e){toast(e.message)} }
if(T) boot();
setInterval(()=>{ if(T && $('modal').classList.contains('hidden')) refresh(); }, 5000);
</script>
</body>
</html>`
