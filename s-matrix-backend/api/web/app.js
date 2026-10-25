const TOKEN_KEY='s_matrix_token';
const $=(s,r=document)=>r.querySelector(s);const $$=(s,r=document)=>Array.from(r.querySelectorAll(s));
const state={token:localStorage.getItem(TOKEN_KEY)||'',inbounds:[],status:{},tab:'nodes',shareLinks:[],subscription:`${location.origin}/api/v1/sub/default`,ws:null,statusTimer:null};
function esc(s){return String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function randHex(n){const b=new Uint8Array(n);crypto.getRandomValues(b);return Array.from(b,x=>x.toString(16).padStart(2,'0')).join('')}
function uuid(){return crypto.randomUUID?crypto.randomUUID():`${randHex(4)}${randHex(2)}-${randHex(2)}-${randHex(2)}-${randHex(2)}-${randHex(6)}`}
function password(n=28){const c='ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789_-@#%';const b=new Uint8Array(n);crypto.getRandomValues(b);return Array.from(b,x=>c[x%c.length]).join('')}
async function api(path,opt={}){const h=new Headers(opt.headers||{});h.set('Content-Type',h.get('Content-Type')||'application/json');if(state.token)h.set('Authorization',`Bearer ${state.token}`);const r=await fetch(path,{...opt,headers:h});if(r.status===401){localStorage.removeItem(TOKEN_KEY);state.token='';renderLogin();throw new Error('unauthorized')}return r}
function toast(t){let e=$('.toast');if(!e){e=document.createElement('div');e.className='toast';document.body.appendChild(e)}e.textContent=t;setTimeout(()=>e.remove(),1600)}
function setAlert(kind,msg){const box=$('#alerts');if(!box)return;box.innerHTML=msg?`<div class="alert ${kind}">${esc(msg)}</div>`:''}
async function login(ev){ev.preventDefault();const u=$('#username').value,p=$('#password').value;try{const r=await fetch('/api/v1/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:u,password:p})});const text=await r.text();if(!r.ok)throw new Error(text);const d=JSON.parse(text);state.token=d.token;localStorage.setItem(TOKEN_KEY,d.token);await renderApp()}catch(e){$('#loginError').textContent=String(e);$('#loginError').classList.remove('hidden')}}
function renderLogin(){
  if(state.statusTimer){clearInterval(state.statusTimer);state.statusTimer=null}
  if(state.ws){state.ws.close();state.ws=null}
  $('#app').innerHTML=`<main class="login-shell"><section class="login-card">
    <div class="brand-row"><img src="/web/favicon.svg" alt=""><div><h1>S-Matrix</h1><p class="lsub">sing-box 管理面板</p></div></div>
    <form class="stack" id="loginForm">
      <input id="username" class="input" value="admin" placeholder="用户名" autocomplete="username">
      <input id="password" class="input" value="admin" type="password" placeholder="密码" autocomplete="current-password">
      <button class="btn primary block" type="submit">登录</button>
    </form>
    <pre id="loginError" class="alert error hidden" style="margin:12px 0 0"></pre>
  </section></main>`;
  $('#loginForm').addEventListener('submit',login);
}
function nav(t){
  state.tab=t;
  $$('.tab').forEach(x=>x.classList.toggle('hidden',x.dataset.tab!==t));
  $$('[data-nav]').forEach(x=>x.classList.toggle('active',x.dataset.nav===t));
  if(t==='terminal')openTerminal();
  if(t==='settings'){loadSingboxVersion();loadAcmeStatus()}
}
function typeLabel(t){return({vless:'REALITY',hysteria2:'HY2',hysteria:'Hysteria',vmess:'VMess',trojan:'Trojan',shadowsocks:'SS',socks:'SOCKS',http:'HTTP',tuic:'TUIC',naive:'Naive',shadowtls:'ShadowTLS',anytls:'AnyTLS'}[t]||String(t).toUpperCase())}
function typeClass(t){return t==='vless'?'vless':(t==='hysteria2'||t==='hysteria'||t==='tuic')?'hy':t==='vmess'?'vmess':(t==='trojan'||t==='shadowtls'||t==='anytls'||t==='naive')?'trojan':''}
function protoFromDB(t){return({vless:'reality',hysteria2:'hy2',hysteria:'hysteria',vmess:'vmess',trojan:'trojan',shadowsocks:'ss',socks:'socks',http:'http',tuic:'tuic',naive:'naive',shadowtls:'shadowtls',anytls:'anytls'}[t]||'reality')}
async function loadStatus(){try{const r=await api('/api/v1/system/status');if(r.ok)state.status=await r.json();renderStatus();renderSummary()}catch{}}
async function loadInbounds(){try{const r=await api('/api/v1/inbounds');if(r.ok){const d=await r.json();state.inbounds=d.obj||[];renderSummary();renderNodes()}}catch(e){setAlert('error',String(e))}}
function pct(v){return Number(v||0).toFixed(1).replace(/\.0$/,'')}
function renderStatus(){
  const s=state.status;
  const on=!!s.sing_box_running;
  $('#statusbar').innerHTML=
    `<span class="pill ${on?'ok':'bad'}"><i class="dot"></i>${on?'运行中':'已停止'}</span>`
    +`<span class="pill">CPU ${pct(s.cpu_percent)}%</span>`
    +`<span class="pill">MEM ${pct(s.memory_percent)}%</span>`
    +`<span class="pill">v${esc(s.singbox_version||'---')}</span>`;
}
function startStatusPolling(){if(state.statusTimer)clearInterval(state.statusTimer);state.statusTimer=setInterval(()=>{if(state.token)loadStatus()},3000)}
function renderSummary(){
  const sub=$('#nodeSub');
  if(sub){
    const total=state.inbounds.length;
    const enabled=state.inbounds.filter(x=>x.enabled).length;
    sub.textContent=`${total} 个节点 · ${enabled} 个启用`;
  }
  const box=$('#summary');
  if(!box)return;
  const total=state.inbounds.length,enabled=state.inbounds.filter(x=>x.enabled).length;
  const ports=new Set(state.inbounds.map(x=>x.port).filter(Boolean)).size;
  box.innerHTML='';
  box.dataset.stats=`${total}|${enabled}|${ports}`;
}
function nodeSni(i){
  try{const p=JSON.parse(i.payload||'{}');return p.server_name||p.sni||''}catch(e){return ''}
}
function renderNodes(){
  const q=($('#search')&&$('#search').value||'').trim().toLowerCase();
  const rows=state.inbounds.filter(i=>!q||`${i.tag} ${i.port} ${i.type} ${nodeSni(i)}`.toLowerCase().includes(q));
  const box=$('#nodesList');
  if(!rows.length){
    box.innerHTML=`<div class="empty"><b>${state.inbounds.length?'没有匹配的节点':'还没有节点'}</b>`
      +`${state.inbounds.length?'换个关键词试试':'用右上角「新建节点」开始，一键即可生成可用节点。'}</div>`;
    return;
  }
  box.innerHTML=`<div class="table-wrap"><table class="grid"><thead><tr>
      <th>节点</th><th>协议</th><th>端口</th><th>状态</th><th style="text-align:right">操作</th>
    </tr></thead><tbody>${rows.map(i=>{
      const sni=nodeSni(i);
      return `<tr>
        <td><div class="cell-name"><span class="nm">${esc(i.tag)}</span></div>${sni?`<div class="cell-sub">${esc(sni)}</div>`:''}</td>
        <td><span class="tag ${typeClass(i.type)}">${typeLabel(i.type)}</span></td>
        <td class="mono">${esc(i.port)}</td>
        <td><span class="state ${i.enabled?'on':'off'}"><i class="dot"></i>${i.enabled?'启用':'停用'}</span></td>
        <td><div class="row-actions">
          <button class="icon-btn" data-act="copy" data-id="${i.id}" title="复制链接">链接</button>
          <button class="icon-btn" data-act="qr" data-id="${i.id}" title="二维码">二维码</button>
          <button class="icon-btn" data-act="edit" data-id="${i.id}" title="编辑">编辑</button>
          <button class="icon-btn" data-act="toggle" data-id="${i.id}" title="${i.enabled?'停用':'启用'}">${i.enabled?'停用':'启用'}</button>
          <button class="icon-btn danger" data-act="del" data-id="${i.id}" title="删除">删除</button>
        </div></td>
      </tr>`;
    }).join('')}</tbody></table></div>`;
  bindNodeButtons();
}
function bindNodeButtons(){
  $$('[data-act]').forEach(b=>{
    const id=Number(b.dataset.id), act=b.dataset.act;
    b.onclick=()=>{
      if(act==='toggle')return toggleInbound(id);
      if(act==='edit')return openModal(id);
      if(act==='copy')return copyInbound(id);
      if(act==='qr')return showQR(id);
      if(act==='del')return deleteInbound(id);
    };
  });
}
async function oneClick(kind){setAlert('success','');try{const r=await api(`/api/v1/quick/${kind}`,{method:'POST'});const d=await r.json();if(!r.ok)throw new Error(d.error||'unknown');state.shareLinks=d.links||[];state.subscription=d.subscription||state.subscription;setAlert('success',`${({reality:'REALITY',hy2:'HY2',anytls:'AnyTLS'})[kind]||kind} 已部署，端口 ${d.port}`);showShare('oneclick');await loadInbounds();await loadStatus()}catch(e){setAlert('error',String(e))}}
async function toggleInbound(id){try{await api(`/api/v1/inbounds/${id}/toggle`,{method:'POST'});await loadInbounds();await loadStatus()}catch(e){setAlert('error',String(e))}}
async function deleteInbound(id){if(!confirm('确认删除？'))return;try{await api(`/api/v1/inbounds/${id}`,{method:'DELETE'});toast('已删除');await loadInbounds();await loadStatus()}catch(e){setAlert('error',String(e))}}
async function inboundLinks(id){const r=await api(`/api/v1/inbounds/${id}/links`);const d=await r.json();return d.links||[]}
async function copyInbound(id){try{const l=(await inboundLinks(id))[0];if(!l)throw new Error('no link');await navigator.clipboard.writeText(l);toast('已复制')}catch(e){setAlert('error',String(e))}}
async function showQR(id){try{state.shareLinks=await inboundLinks(id);showShare('inbound')}catch(e){setAlert('error',String(e))}}
function closeModal(){$('#modalRoot').innerHTML=''}
function setFieldsByProto(){const p=$('#fProtocol').value;const extra=$('#protoFields');const cert='<input id="fCertDomain" class="input" placeholder="自己的域名（指向本机后可申请证书；留空用自签）"><button type="button" class="btn primary" style="width:100%" id="issueDomainCert">校验域名并申请证书</button><input id="fCertPath" class="input" placeholder="certificate_path（必填）"><input id="fKeyPath" class="input" placeholder="key_path（必填）"><button type="button" class="btn secondary" style="width:100%" id="genSelfSigned">生成自签证书并填入路径</button>';const final='<input id="fFinalMask" class="input" placeholder="FinalMask / path / obfs（可选）">';const common={reality:`<input id="fUUID" class="input" placeholder="UUID" value="${uuid()}"><input id="fDest" class="input" placeholder="伪装目标 www.cloudflare.com:443" value="www.cloudflare.com:443"><input id="fSNI" class="input" placeholder="SNI" value="www.cloudflare.com"><input id="fPrivKey" class="input" placeholder="Private Key"><input id="fPubKey" class="input" placeholder="Public Key"><input id="fShortID" class="input" placeholder="Short ID" value="${randHex(8)}">${final}`,hy2:`<input id="fPassword" class="input" placeholder="密码" value="${password()}"><input id="fMasquerade" class="input" placeholder="masquerade URL" value="https://www.bing.com">`,hysteria:`<input id="fPassword" class="input" placeholder="auth_str" value="${password()}"><input id="fUpMbps" class="input" type="number" placeholder="上行 Mbps" value="100"><input id="fDownMbps" class="input" type="number" placeholder="下行 Mbps" value="100"><input id="fObfs" class="input" placeholder="obfs / FinalMask（可选）">${cert}`,vmess:`<input id="fUUID" class="input" placeholder="UUID" value="${uuid()}"><select id="fNetwork" class="select"><option>tcp</option><option>ws</option><option>httpupgrade</option><option>grpc</option><option>http</option></select><input id="fPath" class="input" placeholder="path" value="/"><input id="fHost" class="input" placeholder="host / FinalMask"><select id="fVMessSecurity" class="select"><option>auto</option><option>aes-128-gcm</option><option>chacha20-poly1305</option><option>none</option><option>zero</option></select>`,trojan:`<input id="fPassword" class="input" placeholder="密码" value="${password()}">`,ss:`<select id="fMethod" class="select"><option>aes-128-gcm</option><option>aes-192-gcm</option><option>aes-256-gcm</option><option>chacha20-ietf-poly1305</option><option>xchacha20-ietf-poly1305</option><option>2022-blake3-aes-128-gcm</option><option>2022-blake3-aes-256-gcm</option><option>2022-blake3-chacha20-poly1305</option></select><input id="fPassword" class="input" placeholder="密码" value="${password()}">`,tuic:`<input id="fUUID" class="input" placeholder="UUID" value="${uuid()}"><input id="fPassword" class="input" placeholder="密码" value="${password()}"><select id="fCongestion" class="select"><option>cubic</option><option>bbr</option><option>new_reno</option></select>${cert}`,naive:`<input id="fUsername" class="input" placeholder="用户名" value="user"><input id="fPassword" class="input" placeholder="密码" value="${password()}"><select id="fNetwork" class="select"><option>tcp</option><option>udp</option></select><select id="fCongestion" class="select"><option value="">默认拥塞控制</option><option>bbr</option><option>bbr2</option><option>cubic</option><option>reno</option></select>${cert}`,shadowtls:`<input id="fPassword" class="input" placeholder="密码" value="${password()}"><input id="fSNI" class="input" placeholder="握手域名" value="www.microsoft.com"><select id="fWildcardSNI" class="select"><option value="">wildcard_sni: off</option><option>authed</option><option>all</option></select>`,anytls:`<input id="fPassword" class="input" placeholder="密码" value="${password()}"><input id="fSNI" class="input" placeholder="SNI / 自签证书 CN" value="www.bing.com"><textarea id="fPaddingScheme" class="input" style="height:76px;padding-top:9px" placeholder="padding_scheme，每行一条，可选"></textarea>${cert}`,socks:`<div class="muted small">无需额外参数</div>`,http:`<div class="muted small">无需额外参数</div>`};extra.innerHTML=common[p]||common.reality}
function fillEdit(ib){$('#fRemark').value=ib.tag;$('#fPort').value=ib.port;$('#fProtocol').value=protoFromDB(ib.type);setFieldsByProto();bindProtoExtras();let p={};try{p=JSON.parse(ib.payload||'{}')}catch{};for(const [k,v] of Object.entries({fUUID:p.uuid,fPassword:p.password||p.auth_str,fUsername:p.username,fMethod:p.method,fDest:p.dest,fSNI:p.server_name||p.sni,fPrivKey:p.private_key,fPubKey:p.public_key,fShortID:p.short_id,fNetwork:p.network,fPath:p.path,fHost:p.host||p.finalmask||p.final_mask,fVMessSecurity:p.vmess_security,fMasquerade:p.masquerade,fUpMbps:p.up_mbps,fDownMbps:p.down_mbps,fObfs:p.obfs||p.finalmask||p.final_mask,fCertPath:p.certificate_path||p.cert_path,fKeyPath:p.key_path,fCongestion:p.congestion_control||p.quic_congestion_control,fWildcardSNI:p.wildcard_sni,fFinalMask:p.finalmask||p.final_mask,fPaddingScheme:Array.isArray(p.padding_scheme)?p.padding_scheme.join('\n'):p.padding_scheme})){const el=$('#'+k);if(el&&v!==undefined&&v!==null&&v!=='')el.value=v}}
async function publishHttps(domain){
  const out=$('#domainOut');
  out.textContent=`正在为 ${domain} 配置 HTTPS 反向代理…`;
  try{
    const r=await api('/api/v1/domain/publish',{method:'POST',body:JSON.stringify({domain})});
    const d=await r.json();
    if(!r.ok||d.ok===false)throw new Error(d.error||'failed');
    out.textContent=`已配置 ${domain} → ${d.https_url}\n方式：${d.provider}（${d.config_path}）\n3 秒后跳转到 HTTPS…`;
    toast('HTTPS 已启用');
    setTimeout(()=>{location.href=d.https_url},3000);
  }catch(e){
    out.textContent='配置失败：'+String(e);
    setAlert('error','HTTPS 配置失败：'+String(e));
  }
}
async function saveAcmeEmail(){
  const out=$('#domainOut');const email=($('#acmeEmail')&&$('#acmeEmail').value||'').trim();
  try{
    const r=await api('/api/v1/domain/email',{method:'POST',body:JSON.stringify({ email })});
    const d=await r.json();if(!r.ok||d.ok===false)throw new Error(d.error||'failed');
    out.textContent=d.email?`联系邮箱已保存：${d.email}`:'已清空联系邮箱';
    toast('已保存');
  }catch(e){out.textContent='保存失败：'+String(e);setAlert('error',String(e))}
}
async function loadAcmeStatus(){
  const out=$('#domainOut');out.textContent='读取中…';
  try{
    const r=await api('/api/v1/domain/status');const d=await r.json();
    if(!r.ok)throw new Error(d.error||'failed');
    const st=d.status||{};
    const mode=st.mode==='standalone'?'直连绑定 80 端口（standalone）':'Webroot（由前面的反向代理提供 /.well-known/acme-challenge/）';
    out.textContent=`本机公网地址：${st.server_ip||'未探测到'}\n校验方式：${mode}\nWebroot：${st.webroot||'-'}\n联系邮箱：${st.email||'未设置'}\n证书目录：${st.cert_dir||''}`
        + (st.auto_redirect?`\n面板自带 HTTPS：${st.https_listen}（签发后可直接用 https 访问）`:'');
    if($('#acmeEmail')&&st.email)$('#acmeEmail').value=st.email;
    const r2=await api('/api/v1/domain/certs');const d2=await r2.json();
    const box=$('#certList');
    if(box){
      const certs=(d2&&d2.certs)||[];
      box.innerHTML=certs.length?certs.map(x=>`<div class="cert">
        <span><b>${esc(x.domain)}</b> <span class="muted small">到期 ${esc(String(x.not_after||'').slice(0,10))}</span></span>
        <button class="btn ghost sm" data-publish="${esc(x.domain)}">套用 HTTPS</button>
      </div>`).join(''):'<span class="muted">暂无已签发证书</span>';
      $$('[data-publish]').forEach(b=>b.onclick=()=>publishHttps(b.dataset.publish));
    }
  }catch(e){out.textContent='读取失败：'+String(e)}
}
async function issueDomainCert(){
  const box=$('#fCertDomain');const domain=(box&&box.value||'').trim().toLowerCase();
  if(!domain||!domain.includes('.')){setAlert('error','请先填写指向本机的域名');return}
  const btn=$('#issueDomainCert');const old=btn.textContent;btn.disabled=true;btn.textContent='校验并签发中…';
  try{
    const chk=await api(`/api/v1/domain/check?domain=${encodeURIComponent(domain)}`);
    const cd=await chk.json();
    if(!cd.ok){
      const v=cd.validation||{};
      throw new Error(v.reason||'域名未指向本机，请先改 A 记录');
    }
    const r=await api('/api/v1/domain/issue',{method:'POST',body:JSON.stringify({domain})});
    const d=await r.json();if(!r.ok||d.ok===false)throw new Error(d.error||'failed');
    if($('#fCertPath'))$('#fCertPath').value=d.cert_path;
    if($('#fKeyPath'))$('#fKeyPath').value=d.key_path;
    if($('#fSNI'))$('#fSNI').value=domain;
    setAlert('success',`已为 ${domain} 签发证书（到期 ${String(d.not_after||'').slice(0,10)}）`);
  }catch(e){setAlert('error','申请失败：'+String(e))}
  finally{btn.disabled=false;btn.textContent=old}
}
async function openDomainNodeModal(){
  $('#modalRoot').innerHTML=`<div class="modal-bg"><section class="modal"><div class="modal-head"><h2>一键 AnyTLS（自己的域名）</h2><button class="btn secondary" id="closeModal">关闭</button></div>
  <div class="form-grid">
    <div class="field full"><label>域名</label><input id="dzDomain" class="input" placeholder="完整域名，如 node1.example.com"></div>
    <div class="field"><label>节点备注</label><input id="dzRemark" class="input" placeholder="留空则用域名"></div>
    <div class="field"><label>端口</label><input id="dzPort" class="input" type="number" placeholder="留空自动"></div>
  </div>
  <div class="quick-row"><button class="btn ghost" id="dzCheck">① 校验域名</button></div>
  <p class="muted small" style="margin:8px 0 0">先把该域名的 A 记录指向本机（DNS-only，别开 CDN 代理），再点校验。域名正确后会自动签发 Let's Encrypt 证书并建节点，链接走标准 HTTPS，不需要 insecure。</p>
  <button class="btn primary" style="width:100%;margin-top:12px" id="dzGo" disabled>② 签发证书并创建节点</button>
  <pre id="dzOut" class="terminal" style="height:96px;margin-top:10px">先填域名，点「校验域名」。</pre></section></div>`;
  $('#closeModal').onclick=closeModal;
  $('#dzCheck').onclick=checkDomain;
  $('#dzGo').onclick=runDomainNode;
  try{
    const r=await api('/api/v1/domain/status');const d=await r.json();
    if(r.ok&&d.status)$('#dzOut').textContent=`本机公网地址：${d.status.server_ip||'未探测到'}\n校验方式：${d.status.mode||''}`;
  }catch{}
}
async function checkDomain(){
  const domain=($('#dzDomain')&&$('#dzDomain').value||'').trim().toLowerCase();
  const out=$('#dzOut');
  if(!domain){out.textContent='请先填写域名';return}
  out.textContent='校验中…（检查 DNS 是否指向本机）';
  try{
    const r=await api(`/api/v1/domain/check?domain=${encodeURIComponent(domain)}`);
    const d=await r.json();
    const v=d.validation||{};
    if(!r.ok)throw new Error(d.error||'failed');
    if(d.ok){
      out.textContent=`✅ 校验通过\n${domain} → ${(v.resolved||[]).join(', ')}\n可以签发证书了。`;
      $('#dzGo').disabled=false;
      $('#dzGo').textContent='② 签发证书并创建节点';
    }else{
      out.textContent=`❌ 校验未通过\n${v.reason||'域名未指向本机'}\n解析结果：${(v.resolved||[]).join(', ')||'无'}\n本机地址：${(v.expected||[]).join(', ')||'-'}`;
      $('#dzGo').disabled=true;
    }
  }catch(e){out.textContent='校验失败：'+String(e);$('#dzGo').disabled=true}
}
async function runDomainNode(){
  const domain=($('#dzDomain')&&$('#dzDomain').value||'').trim().toLowerCase();
  const out=$('#dzOut');const btn=$('#dzGo');
  if(!domain){out.textContent='请先填写并校验域名';return}
  btn.disabled=true;btn.textContent='签发中…（约 20-60 秒）';
  out.textContent=`正在为 ${domain} 申请证书…\nLet's Encrypt 会通过 80 端口回源校验，请勿关闭页面。`;
  try{
    const body={domain,remark:($('#dzRemark')&&$('#dzRemark').value||'').trim(),port:Number(($('#dzPort')&&$('#dzPort').value)||0)};
    const r=await api('/api/v1/quick/anytls-domain',{method:'POST',body:JSON.stringify(body)});
    const d=await r.json();
    if(!r.ok||d.ok===false)throw new Error(d.error||'failed');
    out.textContent=`✅ ${d.domain} 已就绪\n端口 ${d.port}\n证书到期 ${String(d.cert_expires||'').slice(0,10)}\n${d.link||''}`
      + (d.publish_error?`\n\n⚠️ HTTPS 未自动套用：${d.publish_error}\n可在「设置 → 域名 / 证书」里点「套用 HTTPS」重试`:(d.auto_redirect?`\n\n已在该域名上启用 HTTPS（${d.site_provider||''}）`:''));
    state.shareLinks=d.link?[d.link]:(d.links||[]);
    state.subscription=d.subscription||state.subscription;
    await loadInbounds();await loadStatus();
    setAlert('success',`${d.domain} 已就绪，端口 ${d.port}`);
    showShare('oneclick');
    if(d.auto_redirect&&d.https_url){
      out.textContent+='\n\n面板自带 HTTPS，3 秒后跳转到 '+d.https_url;
      setTimeout(()=>{location.href=d.https_url},3000);
    }
  }catch(e){out.textContent='失败：'+String(e);setAlert('error',String(e))}
  finally{btn.disabled=false;btn.textContent='② 签发证书并创建节点'}
}
async function genSelfSigned(){try{const sni=($('#fSNI')&&$('#fSNI').value||'www.bing.com').trim();const name=($('#fRemark')&&$('#fRemark').value||'').trim();const r=await api('/api/v1/certs/selfsigned',{method:'POST',body:JSON.stringify({name:name||`self-${sni}`,sni})});const d=await r.json();if(!r.ok)throw new Error(d.error||'failed');if($('#fCertPath'))$('#fCertPath').value=d.cert_path;if($('#fKeyPath'))$('#fKeyPath').value=d.key_path;toast(d.created?'自签证书已生成':'已复用现有自签证书')}catch(e){setAlert('error',String(e))}}
function bindProtoExtras(){const b=$('#genSelfSigned');if(b)b.onclick=genSelfSigned;const d=$('#issueDomainCert');if(d)d.onclick=issueDomainCert}
function openModal(id=0){
  const ib=state.inbounds.find(x=>x.id===id);
  $('#modalRoot').innerHTML=`<div class="modal-bg"><section class="modal">
    <div class="modal-head"><h2>${id?`编辑节点 · ${esc(ib?ib.tag:'')}`:'新增节点'}</h2><button class="btn ghost sm" id="closeModal">关闭</button></div>
    <div class="form-grid">
      <div class="field"><label>备注</label><input id="fRemark" class="input" placeholder="例如 HK-AnyTLS"></div>
      <div class="field"><label>监听端口</label><input id="fPort" class="input" type="number" placeholder="留空自动分配"></div>
      <div class="field full"><label>协议</label>
        <select id="fProtocol" class="select">
          <option value="reality">VLESS REALITY</option><option value="hy2">Hysteria2</option>
          <option value="hysteria">Hysteria v1</option><option value="tuic">TUIC</option>
          <option value="naive">Naive</option><option value="shadowtls">ShadowTLS</option>
          <option value="anytls">AnyTLS</option><option value="vmess">VMess</option>
          <option value="trojan">Trojan</option><option value="ss">Shadowsocks</option>
          <option value="socks">SOCKS5</option><option value="http">HTTP</option>
        </select>
      </div>
      <div id="protoFields"></div>
    </div>
    <div class="quick-row"><span class="small muted" style="align-self:center;margin-right:2px">快捷填充</span>
      <button class="btn ghost sm" id="genUuid">UUID</button>
      <button class="btn ghost sm" id="genPass">密码</button>
      <button class="btn ghost sm" id="genSni">SNI</button>
      <button class="btn ghost sm" id="genKey">REALITY 密钥</button>
    </div>
    <div class="quick-row" style="margin-top:16px;justify-content:flex-end">
      <button class="btn ghost" id="cancelNode">取消</button>
      <button class="btn primary" id="saveNode">${id?'保存修改':'创建节点'}</button>
    </div>
  </section></div>`;
  $('#closeModal').onclick=closeModal;
  $('#cancelNode').onclick=closeModal;
  $('#fProtocol').onchange=()=>{setFieldsByProto();bindProtoExtras()};
  setFieldsByProto();bindProtoExtras();
  if(ib)fillEdit(ib);
  $('#genUuid').onclick=()=>{const e=$('#fUUID');if(e)e.value=uuid()};
  $('#genPass').onclick=()=>{const e=$('#fPassword');if(e)e.value=password()};
  $('#genSni').onclick=()=>{const s=['www.cloudflare.com','www.lovelive-anime.jp','addons.mozilla.org','www.icloud.com','www.apple.com','www.bing.com'];const v=s[Math.floor(Math.random()*s.length)];const a=$('#fSNI'),d=$('#fDest');if(a)a.value=v;if(d)d.value=v+':443'};
  $('#genKey').onclick=genRealityKeys;
  $('#saveNode').onclick=()=>saveNode(id);
}
async function genRealityKeys(){try{const r=await api('/api/v1/system/gen-reality-keypair');const d=await r.json();if($('#fPrivKey')&&d.private_key)$('#fPrivKey').value=d.private_key;if($('#fPubKey')&&d.public_key)$('#fPubKey').value=d.public_key}catch{}if($('#fShortID')&&!$('#fShortID').value)$('#fShortID').value=randHex(8)}
function val(id){return $('#'+id)?.value||''}
function buildNodeData(){const proto=val('fProtocol'),tag=val('fRemark')||'node',port=Number(val('fPort')||0);let kind='inbound-reality',data={tag,port};if(proto==='hy2'){kind='inbound-hy2';data.password=val('fPassword')||password();data.masquerade=val('fMasquerade')||'https://www.bing.com'}else if(proto==='hysteria'){kind='inbound-hysteria';Object.assign(data,{password:val('fPassword')||password(),up_mbps:Number(val('fUpMbps')||100),down_mbps:Number(val('fDownMbps')||100),obfs:val('fObfs'),certificate_path:val('fCertPath'),key_path:val('fKeyPath')})}else if(proto==='tuic'){kind='inbound-tuic';Object.assign(data,{uuid:val('fUUID')||uuid(),password:val('fPassword')||password(),congestion_control:val('fCongestion')||'cubic',certificate_path:val('fCertPath'),key_path:val('fKeyPath')})}else if(proto==='naive'){kind='inbound-naive';Object.assign(data,{username:val('fUsername')||'user',password:val('fPassword')||password(),network:val('fNetwork')||'tcp',quic_congestion_control:val('fCongestion'),certificate_path:val('fCertPath'),key_path:val('fKeyPath')})}else if(proto==='shadowtls'){kind='inbound-shadowtls';Object.assign(data,{password:val('fPassword')||password(),server_name:val('fSNI')||'www.microsoft.com',wildcard_sni:val('fWildcardSNI')})}else if(proto==='anytls'){kind='inbound-anytls';Object.assign(data,{password:val('fPassword')||password(),server_name:val('fSNI')||'www.bing.com',padding_scheme:val('fPaddingScheme'),certificate_path:val('fCertPath'),key_path:val('fKeyPath')})}else if(proto==='vmess'){kind='inbound-vmess';Object.assign(data,{uuid:val('fUUID')||uuid(),network:val('fNetwork'),path:val('fPath'),host:val('fHost'),vmess_security:val('fVMessSecurity'),finalmask:val('fHost')})}else if(proto==='trojan'){kind='inbound-trojan';Object.assign(data,{password:val('fPassword')||password()})}else if(proto==='ss'){kind='inbound-ss';Object.assign(data,{method:val('fMethod'),password:val('fPassword')||password()})}else if(proto==='socks'){kind='inbound-socks'}else if(proto==='http'){kind='inbound-http'}else{Object.assign(data,{uuid:val('fUUID')||uuid(),dest:val('fDest'),server_name:val('fSNI'),private_key:val('fPrivKey'),public_key:val('fPubKey'),short_id:val('fShortID')||randHex(8),finalmask:val('fFinalMask')})}return{kind,data}}
async function saveNode(editId){try{const {kind,data}=buildNodeData();const nodes=[];let i=0;for(const ib of state.inbounds){if(editId&&ib.id===editId)continue;const p=protoFromDB(ib.type);const ed={tag:ib.tag,port:ib.port};try{Object.assign(ed,JSON.parse(ib.payload||'{}'))}catch{};nodes.push({id:`keep-${i++}`,kind:`inbound-${p==='reality'?'reality':p}`,label:ib.tag,data:ed})}nodes.push({id:`new-${i}`,kind,label:data.tag,data});if(editId)await api(`/api/v1/inbounds/${editId}`,{method:'DELETE'});const r=await api('/api/v1/singbox/compile',{method:'POST',body:JSON.stringify({nodes,edges:[]})});if(!r.ok)throw new Error(await r.text());closeModal();toast(editId?'已更新':'已创建');await loadInbounds();await loadStatus()}catch(e){setAlert('error',String(e))}}
async function connectSubgo(){const out=$('#subgoOut');out.textContent='正在对接到 subgo…';try{const r=await api('/api/v1/panel/connect-sub',{method:'POST',body:JSON.stringify({subgo_url:($('#subgoUrl').value||'').trim(),username:($('#subgoUser').value||'').trim(),password:$('#subgoPass').value,name:($('#subgoName').value||'').trim()})});const d=await r.json();if(!r.ok||d.ok===false)throw new Error(d.error||'failed');out.textContent=`已写入 subgo（${d.via}）\n源名称：${d.name}\n源 ID：${d.source_id??'-'}\n面板地址：${d.panel_url}`;toast('已对接 subgo')}catch(e){out.textContent='对接失败：'+String(e);setAlert('error',String(e))}}
async function copyPanelToken(){try{const r=await api('/api/v1/panel/token');const d=await r.json();if(!r.ok)throw new Error(d.error||'failed');await navigator.clipboard.writeText(d.token||'');toast('面板 Token 已复制')}catch(e){setAlert('error',String(e))}}
function showShare(mode){
  const qr=(link)=>`/api/v1/qr.png?size=132&data=${encodeURIComponent(link)}`;
  $('#modalRoot').innerHTML=`<div class="modal-bg"><section class="modal">
    <div class="modal-head"><h2>${mode==='oneclick'?'部署完成':'分享节点'}</h2><button class="btn ghost sm" id="closeModal">关闭</button></div>
    ${mode==='oneclick'?`<div class="card" style="margin-bottom:12px"><div class="card-body">
      <div class="field"><label>订阅地址</label><div class="mono break small">${esc(state.subscription)}</div></div>
      <button class="btn ghost sm" id="copySub" style="margin-top:8px">复制订阅</button>
    </div></div>`:''}
    <div class="share-list">${state.shareLinks.map(l=>`<div class="share">
      <div><div class="small muted" style="margin-bottom:6px">节点链接</div>
        <div class="link">${esc(l)}</div>
        <button class="btn ghost sm" data-copylink="${esc(l)}" style="margin-top:8px">复制链接</button>
      </div>
      <div class="qr"><img src="${qr(l)}" alt="QR"></div>
    </div>`).join('')}</div>
  </section></div>`;
  $('#closeModal').onclick=closeModal;
  if($('#copySub'))$('#copySub').onclick=()=>navigator.clipboard.writeText(state.subscription).then(()=>toast('已复制'));
  $$('[data-copylink]').forEach(b=>b.onclick=()=>navigator.clipboard.writeText(b.dataset.copylink).then(()=>toast('已复制')));
}
async function changePassword(){try{const r=await api('/api/v1/system/change-password',{method:'POST',body:JSON.stringify({username:val('setUser'),old_password:val('oldPass'),new_password:val('newPass')})});if(!r.ok)throw new Error((await r.json()).error||'修改失败');$('#oldPass').value='';$('#newPass').value='';setAlert('success','密码已更新')}catch(e){setAlert('error',String(e))}}
async function loadSingboxVersion(){const out=$('#singboxVersionOut');try{if(out)out.textContent='正在检测 sing-box 版本…';const r=await api('/api/v1/system/singbox/version');const d=await r.json();if(!r.ok)throw new Error(d.error||'检测失败');const set=(id,v)=>{const el=$('#'+id);if(el)el.textContent=v||'--'};set('sbCur',d.current);set('sbStable',d.stable);set('sbAlpha',d.alpha);const sel=$('#sbVersionSelect');if(sel){sel.innerHTML=(d.versions||[]).map(x=>`<option value="${esc(x.tag)}">${esc(x.tag)}${x.prerelease?' · alpha':' · stable'}</option>`).join('');if(d.stable)sel.value=d.stable}if(out)out.textContent='已检测到 '+(d.versions||[]).length+' 个版本'}catch(e){if(out)out.textContent=String(e);setAlert('error',String(e))}}
async function switchSingboxVersion(channel){const out=$('#singboxVersionOut');try{const version=channel==='selected'?($('#sbVersionSelect')?.value||''):'';if(out)out.textContent='正在切换 sing-box '+(version||channel)+'，请稍等…';const r=await api('/api/v1/system/singbox/switch',{method:'POST',body:JSON.stringify({channel,version})});const d=await r.json();if(!r.ok)throw new Error(d.error||'切换失败');if(out)out.textContent='已切换到 '+(d.current||d.version);await loadStatus();await loadSingboxVersion();setAlert('success','sing-box 已切换到 '+(d.current||d.version))}catch(e){if(out)out.textContent=String(e);setAlert('error',String(e))}}
function openTerminal(){if(state.ws)return;const proto=location.protocol==='https:'?'wss:':'ws:';state.ws=new WebSocket(`${proto}//${location.host}/api/v1/logs/ws?token=${encodeURIComponent(state.token)}`);state.ws.onmessage=ev=>{const b=$('#terminalBox');if(!b)return;const line=String(ev.data).trimEnd();const c=/ERROR|FATAL|panic/i.test(line)?'log-error':/WARN|WARNING/i.test(line)?'log-warn':/INFO/i.test(line)?'log-info':'';const n=b.children.length+1;b.insertAdjacentHTML('beforeend',`<div class="${c}"><span style="color:#475569;margin-right:8px">${String(n).padStart(4,'0')}</span>${esc(line)}</div>`);while(b.children.length>600)b.firstChild.remove();b.scrollTop=b.scrollHeight};state.ws.onerror=()=>{$('#terminalBox')?.insertAdjacentHTML('beforeend','<div class="log-error">ERROR websocket telemetry disconnected</div>')}}
async function renderApp(){
  $('#app').innerHTML=`<main class="app">
  <header class="topbar"><div class="top-inner">
    <div class="brand"><img src="/web/favicon.svg" alt=""><b>S-Matrix</b></div>
    <nav class="nav">
      <button data-nav="nodes" class="active">节点</button>
      <button data-nav="subs">订阅</button>
      <button data-nav="settings">设置</button>
      <button data-nav="terminal">日志</button>
    </nav>
    <div class="top-right">
      <div id="statusbar" class="pills"></div>
      <button class="btn text" id="logout">退出</button>
    </div>
  </div></header>

  <div class="page">
    <div id="alerts"></div>

    <div class="tab" data-tab="nodes">
      <div class="page-head">
        <div><h1>节点</h1><p class="sub" id="nodeSub">加载中…</p></div>
        <div class="head-actions">
          <input id="search" class="input search" placeholder="搜索名称 / 端口 / 域名">
          <button class="btn ghost" id="refreshNodes">刷新</button>
          <div class="menu">
            <button class="btn primary" id="createBtn">新建节点</button>
            <div class="menu-pop hidden" id="createMenu">
              <div class="hint">一键创建（推荐）</div>
              <button data-quick="domain">AnyTLS · 域名证书</button>
              <button data-quick="anytls">AnyTLS · 自签证书</button>
              <button data-quick="reality">REALITY</button>
              <button data-quick="hy2">Hysteria2</button>
              <div class="sep"></div>
              <button data-quick="custom">自定义参数…</button>
            </div>
          </div>
        </div>
      </div>
      <div id="summary" class="hidden"></div>
      <section class="card"><div id="nodesList" class="card-body tight"></div></section>
    </div>

    <div class="tab hidden" data-tab="subs">
      <div class="page-head"><div><h1>订阅</h1><p class="sub">把节点分发给客户端，或对接 subgo。</p></div></div>
      <section class="card">
        <div class="card-head"><h2>订阅地址</h2><button class="btn ghost sm" id="copySub2">复制</button></div>
        <div class="card-body">
          <div class="kv">
            <div><span>面板</span><b class="mono break">${esc(location.origin)}</b></div>
            <div><span>订阅</span><b class="mono break">${esc(state.subscription)}</b></div>
          </div>
        </div>
      </section>
      <section class="card">
        <div class="card-head"><h2>对接 subgo</h2></div>
        <div class="card-body">
          <p class="muted small" style="margin:0 0 12px">把本面板作为 SBUI 源写入 subgo，自动带上面板地址与永久 Token。</p>
          <div class="grid2">
            <div class="field"><label>subgo 地址</label><input id="subgoUrl" class="input" placeholder="https://你的-subgo-域名"></div>
            <div class="field"><label>源名称</label><input id="subgoName" class="input" value="S-Matrix"></div>
            <div class="field"><label>用户名</label><input id="subgoUser" class="input" value="admin"></div>
            <div class="field"><label>密码</label><input id="subgoPass" class="input" type="password" placeholder="subgo 密码"></div>
          </div>
          <div class="quick-row">
            <button class="btn primary" id="connectSubgo">一键对接</button>
            <button class="btn ghost" id="copyPanelToken">复制面板 Token</button>
          </div>
          <pre id="subgoOut" class="terminal short" style="margin-top:12px">填写 subgo 账号后点「一键对接」。</pre>
        </div>
      </section>
    </div>

    <div class="tab hidden" data-tab="settings">
      <div class="page-head"><div><h1>设置</h1><p class="sub">账号、证书与 sing-box 版本。</p></div>
        <button class="btn ghost" id="refreshStatus">刷新状态</button></div>

      <section class="card">
        <div class="card-head"><h2>面板账号</h2></div>
        <div class="card-body">
          <div class="grid2">
            <div class="field"><label>用户名</label><input id="setUser" class="input" value="admin"></div>
            <div class="field"><label>旧密码</label><input id="oldPass" class="input" type="password" placeholder="首次修改可留空"></div>
            <div class="field"><label>新密码</label><input id="newPass" class="input" type="password" placeholder="至少 6 位"></div>
          </div>
          <button class="btn primary" id="changePass" style="margin-top:12px">更新密码</button>
        </div>
      </section>

      <section class="card">
        <div class="card-head"><h2>域名 / 证书</h2><button class="btn ghost sm" id="loadAcmeStatus">刷新</button></div>
        <div class="card-body">
          <p class="muted small" style="margin:0 0 12px">自备域名：把 A 记录指向本机（DNS-only，别开 CDN）即可申请证书，全程 HTTP-01，不需要任何 DNS Token。</p>
          <div class="grid2">
            <div class="field"><label>联系邮箱（可选）</label><input id="acmeEmail" class="input" placeholder="用于证书到期提醒"></div>
            <div class="field"><label>&nbsp;</label><button class="btn ghost" id="saveAcmeEmail">保存邮箱</button></div>
          </div>
          <pre id="domainOut" class="terminal short" style="margin-top:12px">点「刷新」查看本机地址与校验方式。</pre>
          <div id="certList" class="cert-list" style="margin-top:10px"></div>
        </div>
      </section>

      <section class="card">
        <div class="card-head"><h2>sing-box 版本</h2><button class="btn ghost sm" id="refreshSingboxVersion">检测版本</button></div>
        <div class="card-body">
          <div class="stats">
            <div class="stat"><div class="n" id="sbCur">--</div><div class="l">当前</div></div>
            <div class="stat"><div class="n" id="sbStable">--</div><div class="l">稳定版</div></div>
            <div class="stat"><div class="n" id="sbAlpha">--</div><div class="l">Alpha</div></div>
            <div class="stat"><div class="n">CORE</div><div class="l">/usr/local/bin/sing-box</div></div>
          </div>
          <div class="grid2">
            <select id="sbVersionSelect" class="select"><option>加载中…</option></select>
            <div class="quick-row" style="margin:0">
              <button class="btn primary sm" id="switchSingboxStable">更新稳定版</button>
              <button class="btn ghost sm" id="switchSingboxSelected">切换所选</button>
              <button class="btn ghost sm" id="switchSingboxAlpha">Alpha</button>
            </div>
          </div>
          <pre id="singboxVersionOut" class="terminal short" style="margin-top:12px">点击「检测版本」获取 sing-box release。</pre>
        </div>
      </section>
    </div>

    <div class="tab hidden" data-tab="terminal">
      <div class="page-head"><div><h1>日志</h1><p class="sub">sing-box 实时输出。</p></div></div>
      <section class="card"><div class="card-body"><div id="terminalBox" class="terminal"></div></div></section>
    </div>
  </div>

  <div id="modalRoot"></div></main>`;

  $$('[data-nav]').forEach(b=>b.onclick=()=>nav(b.dataset.nav));
  $('#logout').onclick=()=>{localStorage.removeItem(TOKEN_KEY);state.token='';renderLogin()};
  $('#refreshNodes').onclick=async()=>{await loadStatus();await loadInbounds();startStatusPolling()};
  $('#refreshStatus').onclick=loadStatus;
  $('#search').oninput=renderNodes;
  $('#changePass').onclick=changePassword;
  $('#saveAcmeEmail').onclick=saveAcmeEmail;
  $('#loadAcmeStatus').onclick=loadAcmeStatus;
  $('#refreshSingboxVersion').onclick=loadSingboxVersion;
  $('#switchSingboxStable').onclick=()=>switchSingboxVersion('stable');
  $('#switchSingboxAlpha').onclick=()=>switchSingboxVersion('alpha');
  $('#switchSingboxSelected').onclick=()=>switchSingboxVersion('selected');
  $('#connectSubgo').onclick=connectSubgo;
  $('#copyPanelToken').onclick=copyPanelToken;
  $('#copySub2').onclick=()=>navigator.clipboard.writeText(state.subscription).then(()=>toast('已复制'));

  // 新建节点菜单
  const menu=$('#createMenu');
  $('#createBtn').onclick=(e)=>{e.stopPropagation();menu.classList.toggle('hidden')};
  if(!window.__menuBound){
    window.__menuBound=true;
    document.addEventListener('click',(e)=>{
      const m=$('#createMenu');
      if(m&&!e.target.closest('.menu'))m.classList.add('hidden');
    });
  }
  $$('#createMenu [data-quick]').forEach(b=>b.onclick=()=>{
    menu.classList.add('hidden');
    const k=b.dataset.quick;
    if(k==='custom')return openModal();
    if(k==='domain')return openDomainNodeModal();
    oneClick(k);
  });
  // 快捷键：/ 聚焦搜索，n 新建
  document.addEventListener('keydown',(e)=>{
    if(e.target.matches('input,textarea,select'))return;
    if(e.key==='/'){e.preventDefault();$('#search').focus()}
    if(e.key==='n'&&!e.metaKey&&!e.ctrlKey){e.preventDefault();$('#createBtn').click()}
  });

  await loadStatus();
  await loadInbounds();
  startStatusPolling();
}

if(state.token)renderApp();else renderLogin();
