package panel

// داشبورد فارسی RTL — تعبیه‌شده در باینری پنل (بدون وابستگی)
const dashboardHTML = `<!DOCTYPE html>
<html lang="fa" dir="rtl">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>PhoenixTunnel Panel</title>
<style>
:root{--bg:#0b0e14;--card:#151a23;--line:#232b3a;--txt:#e6edf3;--dim:#8b98a9;--acc:#2f81f7;--ok:#3fb950;--bad:#f85149}
*{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--txt);font-family:Tahoma,Vazirmatn,sans-serif;min-height:100vh}
header{display:flex;align-items:center;justify-content:space-between;padding:16px 28px;border-bottom:1px solid var(--line);background:var(--card)}
h1{font-size:18px}.h1 span{color:var(--acc)}
.wrap{max-width:1200px;margin:24px auto;padding:0 20px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(320px,1fr));gap:16px}
.card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:20px}
.card h3{font-size:14px;color:var(--dim);margin-bottom:12px}
.big{font-size:28px;font-weight:bold}
.dot{display:inline-block;width:9px;height:9px;border-radius:50%;margin-left:6px}
.on{background:var(--ok)}.off{background:var(--bad)}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{padding:10px;border-bottom:1px solid var(--line);text-align:right}
th{color:var(--dim);font-weight:normal}
input,select,button{background:#0d1117;border:1px solid var(--line);color:var(--txt);border-radius:8px;padding:9px 12px;font-family:inherit;font-size:13px}
button{background:var(--acc);border:none;cursor:pointer;font-weight:bold}
button.ghost{background:transparent;border:1px solid var(--line)}
.row{display:flex;gap:10px;flex-wrap:wrap;margin-top:12px}
#login{max-width:380px;margin:12vh auto}
.err{color:var(--bad);font-size:12px;margin-top:8px}
.hidden{display:none}
.statbar{display:flex;justify-content:space-between;font-size:12px;color:var(--dim);margin-top:6px}
.mono{font-family:monospace;direction:ltr;display:inline-block}
.tag{font-size:11px;background:#21262d;border-radius:20px;padding:2px 10px;margin-left:4px}
</style>
</head>
<body>
<header>
  <h1>🐦‍🔥 <span>PhoenixTunnel</span> — پنل کنترل هسته</h1>
  <div id="who"></div>
</header>

<div id="login" class="card">
  <h3>ورود به پنل</h3>
  <input id="u" placeholder="نام کاربری" style="width:100%"><br><br>
  <input id="p" type="password" placeholder="رمز عبور" style="width:100%"><br>
  <div class="err" id="lerr"></div>
  <div class="row"><button style="width:100%" onclick="doLogin()">ورود</button></div>
</div>

<div id="app" class="wrap hidden">
  <div class="grid" id="kpi"></div>
  <div class="card" style="margin-top:16px">
    <div style="display:flex;justify-content:space-between;align-items:center">
      <h3 style="margin:0">نودهای سرور</h3>
      <button class="ghost" onclick="showNewNode()">+ افزودن نود</button>
    </div>
    <table style="margin-top:12px">
      <thead><tr><th>نام</th><th>مکان</th><th>آی‌پی</th><th>وضعیت</th><th>موتورها</th><th></th></tr></thead>
      <tbody id="nodes"></tbody>
    </table>
    <div id="newnode" class="hidden row">
      <input id="nn_name" placeholder="نام نود">
      <input id="nn_loc" placeholder="مکان (IR/DE/...)">
      <input id="nn_ip" placeholder="آی‌پی">
      <button onclick="createNode()">ساخت نود</button>
    </div>
  </div>
  <div class="card" style="margin-top:16px">
    <h3>اینباندها</h3>
    <table><thead><tr><th>پروتکل</th><th>نود</th><th>پورت</th><th>وضعیت</th></tr></thead>
    <tbody id="inbounds"></tbody></table>
  </div>
</div>

<script>
let T = localStorage.getItem('phx_t') || '';
async function api(m, p, b){
  const r = await fetch(p, {method:m, headers:{'Authorization':'Bearer '+T,'Content-Type':'application/json'}, body:b?JSON.stringify(b):undefined});
  if(r.status===401){logout(); throw new Error('401')}
  return r.json();
}
async function doLogin(){
  const r = await fetch('/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({username:u.value,password:p.value})});
  const j = await r.json();
  if(j.token){T=j.token; localStorage.setItem('phx_t',T); boot();}
  else lerr.textContent='نام کاربری یا رمز اشتباه است';
}
function logout(){localStorage.removeItem('phx_t'); location.reload();}
async function boot(){
  login.classList.add('hidden'); app.classList.remove('hidden');
  who.innerHTML = '<button class="ghost" onclick="logout()">خروج</button>';
  refresh();
}
async function refresh(){
  const nodes = await api('GET','/api/v1/nodes');
  const on = nodes.filter(n=>n.online).length;
  kpi.innerHTML = '<div class="card"><h3>کل نودها</h3><div class="big">'+nodes.length+'</div></div>'
    + '<div class="card"><h3>آنلاین</h3><div class="big" style="color:var(--ok)">'+on+'</div></div>'
    + '<div class="card"><h3>آفلاین</h3><div class="big" style="color:var(--bad)">'+(nodes.length-on)+'</div></div>';
  nodes_tbody.innerHTML = nodes.map(n=>'<tr><td>'+n.name+'</td><td>'+(n.location||'-')+'</td>'
    +'<td class="mono">'+n.ip+'</td>'
    +'<td><span class="dot '+(n.online?'on':'off')+'"></span>'+(n.online?'آنلاین':'آفلاین')+'</td>'
    +'<td>'+(n.engines||[]).map(e=>'<span class="tag">'+e.name+'</span>').join('')+'</td>'
    +'<td><button class="ghost" onclick="switchEngine(\''+n.id+'\')">تعویض موتور</button></td></tr>').join('');
  const ins = await api('GET','/api/v1/inbounds');
  inbounds.innerHTML = ins.map(i=>'<tr><td>'+i.protocol+'</td><td class="mono">'+(i.node_id||'').slice(0,8)+'</td><td class="mono">'+i.port+'</td><td>'+(i.enabled?'<span style="color:var(--ok)">فعال</span>':'غیرفعال')+'</td></tr>').join('');
}
function showNewNode(){newnode.classList.toggle('hidden')}
async function createNode(){
  await api('POST','/api/v1/nodes',{name:nn_name.value,location:nn_loc.value,ip:nn_ip.value,engines:[]});
  newnode.classList.add('hidden'); refresh();
}
async function switchEngine(id){
  const e = prompt('نام موتور جدید (chameleon / hydra / phantom):','phantom');
  if(e) await api('POST','/api/v1/nodes/'+id+'/switch',{engine:e});
}
if(T) boot();
setInterval(()=>{ if(T && !app.classList.contains('hidden')) refresh(); }, 10000);
</script>
</body>
</html>`
