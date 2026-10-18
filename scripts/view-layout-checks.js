async (page, artifactDir) => {
 const base=new URL(page.url()).origin,errors=[];const onError=e=>errors.push(e.message);page.on('pageerror',onError);
 try{
  for(const path of ['/collections','/overview','/converter','/sql','/pipelines','/explorer','/workflow','/settings','/excluded','/not-a-real-page']){
   const response=await page.goto(base+path);if(response.status()!==(path==='/not-a-real-page'?404:200))throw Error('Unexpected page status '+path);
   for(const width of [320,390,768,1024,1440,2560]){await page.setViewportSize({width,height:1000});for(const theme of ['light','dark']){
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
    if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Global page overflow '+path+' '+width);
    const unreadable=await page.evaluate(()=>{
     const rgb=s=>s.match(/[\d.]+/g)?.map(Number),lum=rgb=>rgb.slice(0,3).map(v=>v/255).map(v=>v<=.04045?v/12.92:((v+.055)/1.055)**2.4).reduce((s,v,i)=>s+v*[.2126,.7152,.0722][i],0);
     return [...document.querySelectorAll('.button,.icon-button,h1,h2,h3,p,label,summary')].filter(el=>el.getBoundingClientRect().height&&el.textContent.trim()&&!el.disabled&&el.getAttribute('aria-disabled')!=='true'&&getComputedStyle(el).visibility!=='hidden'&&(!el.closest('details:not([open])')||el===el.closest('details:not([open])').querySelector(':scope>summary'))).flatMap(el=>{const style=getComputedStyle(el);let current=el,bg;while(current){bg=rgb(getComputedStyle(current).backgroundColor);if(bg&&(bg.length<4||bg[3]===1))break;current=current.parentElement;}const fg=rgb(style.color);if(!bg||!fg)return [];const a=lum(fg),b=lum(bg),ratio=(Math.max(a,b)+.05)/(Math.min(a,b)+.05);return ratio<4.5?[el.textContent.trim().slice(0,80)+': '+ratio.toFixed(2)]:[];});
    });if(unreadable.length)throw Error('Unreadable text '+path+' '+theme+': '+unreadable.join(', '));
    if(path==='/collections'){
     const search=page.locator('.search-field');
     const borders=await search.evaluate(el=>({outer:getComputedStyle(el).borderTopWidth,inner:getComputedStyle(el.querySelector('input')).borderTopWidth}));
     if(borders.outer!=='1px'||borders.inner!=='0px')throw Error('Files search must have one border');
     await search.locator('input').focus();
     if(await search.evaluate(el=>getComputedStyle(el).outlineStyle)==='none')throw Error('Search focus ring missing');
    }
    const short=await page.locator('.field-control,.button,.icon-button').evaluateAll(es=>es.filter(e=>e.checkVisibility({checkOpacity:true,checkVisibilityCSS:true})&&!e.closest('details:not([open])')&&!e.disabled&&e.getAttribute('aria-disabled')!=='true').filter(e=>e.getBoundingClientRect().height<(innerWidth<=767?44:40)-.75).map(e=>e.getAttribute('aria-label')||e.name||e.textContent.trim()));
    if(short.length)throw Error('Short controls '+path+' '+width+': '+short.join(', '));
    if([390,1440].includes(width))await page.screenshot({path:artifactDir+'/page-'+path.slice(1)+'-'+width+'-'+theme+'.png'});
    await page.evaluate(()=>document.documentElement.style.zoom='2');if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Zoom overflow '+path+' '+width+' '+JSON.stringify(await page.evaluate(()=>({width:document.documentElement.scrollWidth,viewport:innerWidth,items:[...document.querySelectorAll('body *')].filter(e=>!e.closest('.table-scroll')).filter(e=>e.getBoundingClientRect().right>innerWidth+1).slice(0,5).map(e=>({class:e.className,text:e.textContent.trim().slice(0,30),right:e.getBoundingClientRect().right}))}))));await page.evaluate(()=>document.documentElement.style.zoom='');
   }}
  }
  const themeContext=await page.context().browser().newContext({viewport:{width:1440,height:1000},colorScheme:'light'});
  try{
   const themed=await themeContext.newPage();
   await themed.goto(base+'/collections');
   if(await themed.evaluate(()=>document.documentElement.dataset.theme)!=='light')throw Error('A light system preference was ignored');
   await themed.emulateMedia({colorScheme:'dark'});
   await themed.waitForFunction(()=>document.documentElement.dataset.theme==='dark');
   await themed.evaluate(()=>localStorage.setItem('fluxport-theme','light'));
   await themed.reload();
   if(await themed.evaluate(()=>document.documentElement.dataset.theme)!=='light')throw Error('A stored theme did not beat the system preference');
   await themed.evaluate(()=>{localStorage.removeItem('fluxport-theme');});
   await themed.reload();
   if(await themed.evaluate(()=>document.documentElement.dataset.theme)!=='dark')throw Error('Removing the stored theme did not return to the system preference');
  }finally{await themeContext.close();}
  if(errors.length)throw Error('App page errors: '+errors.join('; '));
  console.log('App-wide layouts passed: library, overview, converter, SQL, pipelines, API, workflow, settings, exclusions and errors; desktop/mobile, themes, text contrast and single-border search.');
 }finally{page.off('pageerror',onError);await page.setViewportSize({width:1440,height:1000});}
}
