// Transient DOM data injection only; reference source is never rewritten.
const fs=require('fs'), path=require('path'), {pathToFileURL}=require('url');
const {chromium}=require('playwright'); const {PNG}=require('pngjs');
(async()=>{
 const source=path.resolve(process.argv[2]),out=path.resolve(process.argv[3]);
 const browser=await chromium.launch({headless:true,args:['--no-sandbox'],env:process.env});
 const page=await browser.newPage({viewport:{width:1280,height:1000},deviceScaleFactor:1});
 const states={authenticated:'connected',suspended:'idle',authenticating:'authenticating',waiting_for_network:'network',waiting_before_retry:'retry',blocked_by_error:'failed',empty:'empty'};
 const selectors={'home-issue':'#issue','home-configuration-button':'#connectionSelector','home-settings-button':'.home-head .icon-btn','home-header':'.home-head','home-status':'.status-block','home-mark':'.status-mark','home-context':'.status-context','home-actions':'#actions','home-divider':'.divider','home-notice':'.notice',DesignHeader:'.app-head',DesignGroup:'.group',Dialog:'.sheet',TextField:'#accountInput'};
 const summary=[];
 for(const name of fs.readdirSync(out).filter(n=>fs.existsSync(path.join(out,n,'flutter.json')))) {
  const dir=path.join(out,name),data=JSON.parse(fs.readFileSync(path.join(dir,'flutter.json')));
  await page.goto(pathToFileURL(source).href);
  await page.selectOption('#size','reference'); await page.selectOption('#scenario',states[data.scenario]); await page.selectOption('#feed','active');await page.selectOption('#themeMode',data.theme);
  if(data.page==='account') await page.locator('#connectionSelector').click();
  if(data.page==='settings') await page.locator('.home-head [data-go="settings"]').click();
  if(['details','diagnostics'].includes(data.page)){await page.locator('#secondaryAction').click();if(data.page==='diagnostics')await page.locator('[data-page="details"] [data-go="diagnostics"]').click();}
  if(data.page==='modal')await page.locator('#noticeButton').click();
  await page.evaluate(d=>{
    if(d.home){document.querySelector('#statusDetail').textContent=d.home.detail; document.querySelector('#contextNote').textContent=d.home.context;}
    for(const r of d.rows){if(r.value==null)continue;for(const row of document.querySelectorAll(`[data-page="${d.page}"] .row`)){if(row.querySelector('strong')?.textContent===r.title) row.querySelector('.row-value').textContent=r.value;}}
    if(d.page==='modal'){document.querySelector('.web-page').innerHTML='<h3>校园网维护安排</h3><p>这是离线演示公告。配置、导航与连接操作仅使用内存模拟数据，不会访问校园网或真实账号。</p>';}
    if(d.page==='settings'){document.querySelector('#autoLogin').checked=false; document.querySelector('#autoReconnect').checked=false; document.querySelector('#autoReconnect').disabled=true; document.querySelector('#autoReconnect').closest('.row').querySelector('.row-copy span').textContent='后端能力暂不可用';}
    if(d.page==='home' && d.scenario==='blocked_by_error')document.querySelector('#issue code').textContent='credential_invalid';
    const window=document.querySelector('#window');const r=window.getBoundingClientRect();window.style.transform=`translate(${Math.round(r.x)-r.x}px,${Math.round(r.y)-r.y}px)`;
  },data);
  await page.evaluate(()=>document.fonts.ready); await page.waitForTimeout(200);
  const view=await page.locator('.view').boundingBox(),outer=await page.locator('#window').boundingBox();
  await page.screenshot({path:path.join(dir,'reference.png'),clip:view}); await page.screenshot({path:path.join(dir,'reference-window.png'),clip:outer});
  const rects=await page.evaluate(({selectors,page})=>{const visible=page==='home'?'[data-page="home"]':`[data-page="${page}"]`;const result={};for(const [k,s] of Object.entries(selectors)){const el=document.querySelector(k==='Dialog'?s:`${visible} ${s}`);if(el && el.getClientRects().length){const r=el.getBoundingClientRect();result[k]={x:r.x,y:r.y,width:r.width,height:r.height};}}return result;},{selectors,page:data.page==='modal'?'home':data.page});
  fs.writeFileSync(path.join(dir,'reference.json'),JSON.stringify({outer,view,rects,theme:data.theme,browser:browser.version()},null,2));
  const a=PNG.sync.read(fs.readFileSync(path.join(dir,'reference.png'))),b=PNG.sync.read(fs.readFileSync(path.join(dir,'flutter.png')));
  if(a.width!==b.width||a.height!==b.height)throw Error('Capture size mismatch '+name);
  const overlay=new PNG({width:a.width,height:a.height}),diff=new PNG({width:a.width,height:a.height});let changed=0;
  for(let i=0;i<a.data.length;i+=4){for(let c=0;c<3;c++){overlay.data[i+c]=Math.round((a.data[i+c]+b.data[i+c])/2);diff.data[i+c]=Math.abs(a.data[i+c]-b.data[i+c]);changed+=a.data[i+c]!==b.data[i+c];}overlay.data[i+3]=diff.data[i+3]=255;}
  fs.writeFileSync(path.join(dir,'overlay.png'),PNG.sync.write(overlay));fs.writeFileSync(path.join(dir,'difference.png'),PNG.sync.write(diff));
  const deltas={};let table='| Element | HTML x,y,w,h | Flutter x,y,w,h | Delta x,y,w,h |\n|---|---|---|---|\n';
  for(const [key,f] of Object.entries(data.elements)){if(!rects[key])continue;const r={...rects[key],x:rects[key].x-view.x,y:rects[key].y-view.y};const d=Object.fromEntries(Object.keys(r).map(k=>[k,f[k]-r[k]]));deltas[key]=d;const fmt=v=>Object.values(v).map(x=>x.toFixed(3)).join(', ');table+=`|${key}|${fmt(r)}|${fmt(f)}|${fmt(d)}|\n`;}
  fs.writeFileSync(path.join(dir,'geometry.md'),table);summary.push({name,changedChannels:changed,deltas});
 }
 fs.writeFileSync(path.join(out,'comparison.json'),JSON.stringify({sourceSHA256:require('crypto').createHash('sha256').update(fs.readFileSync(source)).digest('hex'),environment:'WSL Chromium DPR1; Flutter Windows widget configuration; HarmonyOS vs Segoe/YaHei; candidates only',cases:summary},null,2));
 console.log(JSON.stringify({cases:summary.length,browser:browser.version()}));await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
