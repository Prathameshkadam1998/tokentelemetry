'use strict';
const $=id=>document.getElementById(id);
const fmt=n=>Number.isFinite(n)?new Intl.NumberFormat().format(n):'Unavailable';
const bytes=n=>Number.isFinite(n)?`${(n/1073741824).toFixed(1)} GiB`:'Unavailable';
const when=t=>t&&Date.parse(t)>0?new Date(t).toLocaleString():'Unavailable';
const speed=e=>e?.output>0&&e?.eval_ns>0?e.output*1e9/e.eval_ns:null;
let latest=null,busy=false;
function node(tag,text,cls){const el=document.createElement(tag);if(text!==undefined)el.textContent=text;if(cls)el.className=cls;return el;}
function cell(row,text,sub){const c=node('td',text);if(sub)c.append(node('small',sub));row.append(c);return c;}
function empty(id,text,cols=5){const row=node('tr');const c=cell(row,text);c.colSpan=cols;c.className='empty';$(id).replaceChildren(row);}
async function api(path,options){const r=await fetch(path,options);if(!r.ok)throw new Error(await r.text());return r.json();}
function notice(text){$('notice').textContent=text;$('notice').hidden=!text;}
function modelRows(s){
 const rows=$('model-rows');rows.replaceChildren();const models=new Map((s.installed||[]).map(m=>[m.name,m]));for(const m of s.models||[])models.set(m.name,m);
 if(!models.size){empty('model-rows',s.connected?'No installed models reported. Install a model on the Mac to get started.':'Ollama is unavailable. Check connection settings.');return;}
 for(const m of models.values()){
  const loaded=(s.models||[]).some(a=>a.name===m.name),row=node('tr');cell(row,m.name);cell(row,loaded?'Loaded':s.connected?'Installed · idle':'Loaded state unknown');cell(row,`${m.details?.parameter_size||'Unknown'} / ${m.details?.quantization_level||'Unknown'}`);
  cell(row,loaded?bytes(m.size):'—',loaded?`GPU allocation: ${bytes(m.size_vram)}`:`On disk: ${bytes(m.size)}`);
  cell(row,loaded&&m.context_length?`${fmt(m.context_length)} tokens`:'Unavailable',loaded?'Reported by /api/ps':'Load a model to observe its runtime window');rows.append(row);
 }
}
function filteredEvents(){let events=latest?.events||[],p=$('period').value;const from=new Date();if(p==='today'){from.setHours(0,0,0,0);}else if(p==='7'){from.setDate(from.getDate()-7);}return p==='all'?events:events.filter(e=>Date.parse(e.at)>=from.getTime());}
function usageRows(){
 const events=filteredEvents(),groups=new Map();let output=0,knownOutput=0;
 for(const e of events){let g=groups.get(e.model);if(!g){g={n:0,input:0,output:0,ki:0,ko:0,decode:0,ns:0,failed:0};groups.set(e.model,g);}g.n++;if(e.state!=='complete')g.failed++;
  if(Number.isFinite(e.input)){g.input+=e.input;g.ki++;}if(Number.isFinite(e.output)){g.output+=e.output;g.ko++;output+=e.output;knownOutput++;}if(speed(e)!==null){g.decode+=e.output;g.ns+=e.eval_ns;}
 }
 $('output').textContent=knownOutput?fmt(output):'—';$('output-detail').textContent=`${fmt(events.length)} retained requests · ${events.length-knownOutput} without output counts`;
 const rows=$('usage-rows');rows.replaceChildren();const max=Math.max(1,...Array.from(groups.values(),g=>g.output));
 for(const [model,g]of groups){const row=node('tr');cell(row,model);cell(row,fmt(g.n),g.failed?`${g.failed} incomplete or failed`:undefined);cell(row,g.ki?fmt(g.input):'Unavailable',g.ki<g.n?`${g.n-g.ki} unmeasured`:undefined);const c=cell(row,g.ko?fmt(g.output):'Unavailable',g.ko<g.n?`${g.n-g.ko} unmeasured`:undefined);c.className='barcell';if(g.ko){const bar=node('progress');bar.max=max;bar.value=g.output;bar.setAttribute('aria-label',`${model} output tokens`);c.append(bar);}cell(row,g.ns?`${(g.decode*1e9/g.ns).toFixed(1)} tok/s`:'Unavailable','Weighted by decode time');rows.append(row);}
 if(!groups.size)empty('usage-rows','No requests captured in this period. Point your Ollama client at the laptop proxy below.');
 const recent=$('request-rows');recent.replaceChildren();for(const e of events.slice(-20).reverse()){const row=node('tr');cell(row,when(e.at));cell(row,e.model);cell(row,e.state,e.http_status?`HTTP ${e.http_status}`:undefined);cell(row,`${fmt(e.input)} / ${fmt(e.output)}`);cell(row,e.requested_context?`${fmt(e.requested_context)} tokens`:'Not specified');recent.append(row);}if(!events.length)empty('request-rows','No captured requests.');
}
function render(s){
 latest=s;$('connection').textContent=s.connected?'● Ollama connected':'○ Ollama unavailable';$('connection').className=`badge ${s.connected?'ok':'off'}`;$('live-dot').className=`dot ${s.connected?'':'off'}`;
 $('updated').textContent=`Polled ${when(s.observed_at)} · every 15s`;$('loaded').textContent=s.connected?fmt((s.models||[]).length):'—';$('loaded-detail').textContent=s.connected?`${s.inventory_error?'Installed count unavailable':(s.installed||[]).length+' installed'} · resident does not mean busy`:'No measurement while disconnected';
 const last=(s.events||[]).filter(e=>e.state==='complete'&&speed(e)!==null).at(-1);$('speed').textContent=last?`${speed(last).toFixed(1)}`:'—';$('speed-detail').textContent=last?`tok/s · ${last.model} · ${when(last.at)}`:'Route a request through the proxy';
 const h=s.host||{};$('cpu').textContent=Number.isFinite(h.cpu_percent)?`${h.cpu_percent.toFixed(1)}%`:'Unavailable';$('cpu-bar').value=h.cpu_percent||0;$('cpu-bar').hidden=!Number.isFinite(h.cpu_percent);$('cpu-detail').textContent=h.cpu_percent==null?'No current CPU sample':`Sampled ${when(h.observed_at)}`;$('memory').textContent=bytes(h.memory_total);$('memory-detail').textContent=h.memory_summary||'No current memory sample';$('host-note').textContent=h.error||`${h.source}. Memory use includes caches and compression; it is not a memory-pressure score.`;
 const guidance=$('suggestions');guidance.replaceChildren(...[...(s.suggestions||[]),...(fleet?.suggestions||[])].map(x=>node('p',x)));$('active').textContent=`${Object.values(s.active||{}).reduce((a,b)=>a+b,0)} proxy requests in flight`;
 notice([s.error&&`Ollama: ${s.error}`,s.inventory_error&&`Inventory: ${s.inventory_error}`,s.tunnel_error,s.store_error].filter(Boolean).join(' • '));modelRows(s);usageRows();
}
async function refresh(){if(busy)return;busy=true;$('refresh').disabled=true;try{render(await api('/obs/status'));}catch(e){notice(`Refresh failed. Displayed measurements may be stale: ${e.message}`);$('connection').textContent='Refresh unavailable';$('connection').className='badge off';}finally{busy=false;$('refresh').disabled=false;}}
function modeChanged(){const tunnel=$('mode').value==='ssh';$('endpoint').readOnly=tunnel;if(tunnel)$('endpoint').value='http://127.0.0.1:11435';$('ssh-host').required=tunnel;}
async function loadConfig(){const c=await api('/obs/config');$('mode').value=c.tunnel?'ssh':'direct';$('ssh-host').value=c.ssh_host;$('endpoint').value=c.endpoint;$('min-tps').value=c.min_tps;$('context-warning').value=c.context_warning;$('cpu-warning').value=c.cpu_warning;$('quota-warning').value=c.quota_warning;$('remote-history').checked=!!c.remote_history;$('host-label').textContent=c.ssh_host||'Ollama host';$('endpoint-label').textContent=c.tunnel?'Encrypted SSH tunnel · loopback Ollama':c.endpoint;modeChanged();}
function usageTotal(u={}){return ['input','output','cache_read','cache_write','unclassified'].reduce((n,k)=>n+(u[k]||0),0);}
function renderHistory(h){
 const target=$('history-content');target.className='';target.replaceChildren();if(h.error){target.append(node('p',h.error,'warning'));return;}
 const r=h.report,t=r?.totals;if(!t){target.append(node('p','No accounting report returned.'));return;}
 const listValue=(amount,turns,unpriced)=>turns>0&&unpriced>=turns?'Unavailable':`$${Number(amount||0).toFixed(2)}`;
 const stats=node('div',undefined,'history-stats');for(const [label,value]of [['Measured tokens',fmt(usageTotal(t.usage))],['API list value · priced portion',listValue(t.cost,t.turns,t.unpriced_turns)],['Sessions',fmt(t.sessions)],['Unpriced turns',fmt(t.unpriced_turns)]]){const box=node('div');box.append(node('strong',value),node('small',label));stats.append(box);}target.append(stats);
 target.append(node('p',`Read ${when(h.observed_at)}. Activity dates: ${r.window_from||'unavailable'} to ${r.window_to||'unavailable'}.`, 'footnote'));
 const table=node('table'),head=node('thead'),hr=node('tr');['Model','Measured tokens','API list value · priced portion','Turns','Unpriced turns'].forEach(x=>hr.append(node('th',x)));head.append(hr);table.append(head);const body=node('tbody');for(const m of r.by_model||[]){const row=node('tr');cell(row,m.key);cell(row,fmt(usageTotal(m.usage)));cell(row,listValue(m.cost,m.turns,m.unpriced));cell(row,fmt(m.turns));cell(row,fmt(m.unpriced||0));body.append(row);}table.append(body);const wrap=node('div',undefined,'table-wrap');wrap.append(table);target.append(wrap);
 const billing=t.cost_by_billing||{};target.append(node('p',`List value by billing class: ${Object.entries(billing).map(([k,v])=>`${k}: $${Number(v).toFixed(2)}`).join(' · ')||'Unavailable'}.`, 'footnote'));
 const limits=node('div',undefined,'limits');limits.append(node('h3','Provider-reported plan windows'));
 if(!r.plan_windows?.length)limits.append(node('p','Unavailable: no live provider-reported window was found. Local Ollama does not have a cumulative token allowance.','description'));
 for(const p of r.plan_windows||[])limits.append(node('p',`${p.agent} · ${p.window}: ${p.used_percent}% used. Observed ${when(p.observed_at)}; resets ${when(p.resets_at)}. This is the source’s last report, not a live provider query.`, 'description'));target.append(limits);
 if(h.warnings)target.append(node('p',h.warnings,'warning'));
 const details=node('details'),summary=node('summary','Full accounting evidence, exclusions and caveats');details.append(summary,node('pre',JSON.stringify(r,null,2),'history-details'));target.append(details);
}
$('config-form').addEventListener('submit',async e=>{e.preventDefault();const b=e.submitter;b.disabled=true;$('save-status').textContent='Saving…';try{await api('/obs/config',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({endpoint:$('endpoint').value,ssh_host:$('ssh-host').value,tunnel:$('mode').value==='ssh',min_tps:Number($('min-tps').value),context_warning:Number($('context-warning').value),cpu_warning:Number($('cpu-warning').value),quota_warning:Number($('quota-warning').value),remote_history:$('remote-history').checked})});await loadConfig();$('save-status').textContent='Saved. Connection will refresh shortly.';setTimeout(refresh,1500);}catch(err){$('save-status').textContent=err.message;}finally{b.disabled=false;}});
$('load-history').addEventListener('click',async()=>{const b=$('load-history');b.disabled=true;b.textContent='Reading logs…';try{renderHistory(await api('/obs/history'));}catch(e){$('history-content').textContent=e.message;}finally{b.textContent='Refresh agent history';b.disabled=false;}});

// ---- All sources (laptop + Mac) ----
let fleet=null;
const usd=v=>`$${Number(v||0).toFixed(2)}`;
function fleetRows(){
 const rows=$('fleet-rows');rows.replaceChildren();if(!fleet)return;const want=$('machine-filter').value;let n=0;
 for(const node of fleet.nodes||[]){if(node.error||(want!=='all'&&node.machine!==want))continue;for(const r of node.rows||[]){n++;const row=node_('tr');
  cell(row,node.machine);cell(row,r.agent);cell(row,r.model,r.provider);cell(row,r.billing);
  cell(row,`${fmt(r.usage.input)} / ${fmt(r.usage.output)}`,r.usage.unclassified?`${fmt(r.usage.unclassified)} unclassified`:undefined);
  cell(row,`${fmt(r.usage.cache_read||0)} / ${fmt(r.usage.cache_write||0)}`);
  cell(row,r.unpriced>=r.records?'Unpriced':usd(r.list_value),r.unpriced&&r.unpriced<r.records?`${r.unpriced} of ${r.records} records unpriced`:(r.stale_rated_records?`${r.stale_rated_records} rated with newer prices`:undefined));
  cell(row,when(r.last_recorded),r.last_context?`last request context ${fmt(r.last_context)} tokens`:'context not recorded');rows.append(row);}}
 if(!n)empty('fleet-rows','No measured usage in the last 30 days for this selection.',8);
}
function renderFleet(f){
 fleet=f;const status=$('fleet-status');status.className='';status.replaceChildren();
 for(const n of f.nodes||[]){status.append(node_('p',n.error?`${n.machine}: ${n.error}`:`${n.machine}: read ${when(n.observed_at)} · ${n.from} to ${n.to} · pricing ${n.pricing_date||'unavailable'}${n.duplicates?` · ${n.duplicates} duplicates removed`:''}${n.excluded?` · ${n.excluded} overlapping records excluded`:''}`,n.error?'warning':'footnote'));for(const w of n.warnings||[])status.append(node_('p',`${n.machine}: ${w}`,'warning'));}
 const ok=(f.nodes||[]).filter(n=>!n.error),stats=$('fleet-stats');stats.replaceChildren();stats.hidden=!ok.length;
 const tok=ok.reduce((a,n)=>a+usageTotal(n.totals?.usage),0),models=new Set(),tools=new Set();let value=0,unpriced=0;
 for(const n of ok)for(const r of n.rows||[]){models.add(r.model);tools.add(`${n.machine}/${r.agent}`);value+=r.list_value||0;unpriced+=r.unpriced||0;}
 for(const [label,v]of [['Measured tokens',fmt(tok)],['Models seen',fmt(models.size)],['Tools with usage',fmt(tools.size)],['API list value · priced portion',usd(value)],['Unpriced records',fmt(unpriced)]]){const b=node_('div');b.append(node_('strong',v),node_('small',label));stats.append(b);}
 fleetRows();
 const q=$('quota-rows');q.replaceChildren();
 for(const n of ok)for(const w of n.windows||[]){const row=node_('tr');cell(row,n.machine);cell(row,w.agent,w.window);cell(row,Number.isFinite(w.used_percent)?`${w.used_percent.toFixed(1)}%`:'Unavailable');cell(row,w.expired?'Window has reset':when(w.resets_at));const c=cell(row,w.fresh?'Fresh':'Stale',`observed ${when(w.observed_at)}`);c.className=w.fresh?'fresh':'stale';q.append(row);}
 if(!q.children.length)empty('quota-rows','No tool reported a quota window. That does not mean quota is available.',5);
 const cov=$('coverage-rows');cov.replaceChildren();const by=new Map();
 for(const n of f.nodes||[])for(const c of n.coverage||[]){let e=by.get(c.agent);if(!e){e={agent:c.agent,coverage:c.coverage,explicit:c.explicit_only};by.set(c.agent,e);}e[n.machine]=c;}
 const label=c=>!c?'Not scanned':c.records?`${fmt(c.records)} records · ${when(c.last_recorded)}`:c.detected?'Detected · no recent usage':(c.explicit_only?'Opt-in':'Not found');
 const list=[...by.values()].sort((a,b)=>((b.Laptop?.records||0)+(b.Mac?.records||0))-((a.Laptop?.records||0)+(a.Mac?.records||0))||((b.Laptop?.detected||b.Mac?.detected)?1:0)-((a.Laptop?.detected||a.Mac?.detected)?1:0));
 for(const e of list){const row=node_('tr');cell(row,e.agent);const l=cell(row,label(e.Laptop)),m=cell(row,label(e.Mac));if(!e.Laptop?.detected)l.className='dim';if(!e.Mac?.detected)m.className='dim';cell(row,e.coverage||'—');cov.append(row);}
 if(latest)render(latest);
}
const node_=node;
$('load-fleet').addEventListener('click',async()=>{const b=$('load-fleet');b.disabled=true;b.textContent='Scanning…';$('fleet-status').textContent='Reading laptop and Mac logs. This can take up to a minute…';try{renderFleet(await api(fleet?'/obs/fleet/refresh':'/obs/fleet',fleet?{method:'POST'}:undefined));}catch(e){$('fleet-status').textContent=e.message;}finally{b.textContent='Rescan all sources';b.disabled=false;}});
$('machine-filter').addEventListener('change',fleetRows);
$('install-collector').addEventListener('click',async()=>{const b=$('install-collector');b.disabled=true;$('save-status').textContent='Installing the read-only collector on the Mac over SSH…';try{const r=await api('/obs/collector/install',{method:'POST'});$('save-status').textContent=r.status;}catch(e){$('save-status').textContent=e.message;}finally{b.disabled=false;}});
$('refresh').addEventListener('click',refresh);$('period').addEventListener('change',usageRows);$('mode').addEventListener('change',modeChanged);$('proxy-url').textContent=location.origin;
$('copy-proxy').addEventListener('click',async()=>{try{await navigator.clipboard.writeText(location.origin);$('copy-proxy').textContent='Copied';}catch{$('copy-proxy').textContent='Select the URL to copy';}});
loadConfig().then(refresh).catch(e=>notice(e.message));setInterval(()=>{if(!document.hidden)refresh();},15000);
