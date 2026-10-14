async (page, artifactDir) => {
 const base=new URL(page.url()).origin,errors=[];const onError=e=>errors.push(e.message);page.on('pageerror',onError);
 try{
  for(const path of ['/collections','/overview','/converter','/sql','/pipelines','/explorer','/workflow','/settings','/excluded','/not-a-real-page']){
   const response=await page.goto(base+path);if(response.status()!==(path==='/not-a-real-page'?404:200))throw Error('Unexpected page status '+path);
   for(const width of [390,1440]){await page.setViewportSize({width,height:1000});for(const theme of ['light','dark']){
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
    if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Global page overflow '+path+' '+width);
    const unreadable=await page.evaluate(()=>{
     const rgb=s=>s.match(/[\d.]+/g)?.map(Number),lum=rgb=>rgb.slice(0,3).map(v=>v/255).map(v=>v<=.04045?v/12.92:((v+.055)/1.055)**2.4).reduce((s,v,i)=>s+v*[.2126,.7152,.0722][i],0);
     return [...document.querySelectorAll('.button,.icon-button')].filter(el=>el.getBoundingClientRect().height&&el.textContent.trim()&&!el.disabled&&el.getAttribute('aria-disabled')!=='true'&&getComputedStyle(el).visibility!=='hidden').flatMap(el=>{const style=getComputedStyle(el);let current=el,bg;while(current){bg=rgb(getComputedStyle(current).backgroundColor);if(bg&&(bg.length<4||bg[3]===1))break;current=current.parentElement;}const fg=rgb(style.color);if(!bg||!fg)return [];const a=lum(fg),b=lum(bg),ratio=(Math.max(a,b)+.05)/(Math.min(a,b)+.05);return ratio<4.5?[el.textContent.trim()+': '+ratio.toFixed(2)]:[];});
    });if(unreadable.length)throw Error('Unreadable buttons '+path+' '+theme+': '+unreadable.join(', '));
    await page.screenshot({path:artifactDir+'/page-'+path.slice(1)+'-'+width+'-'+theme+'.png'});
   }}
  }
  if(errors.length)throw Error('App page errors: '+errors.join('; '));
  console.log('App-wide layouts passed: library, overview, converter, SQL, pipelines, API, workflow, settings, exclusions and errors; desktop/mobile, themes and button contrast.');
 }finally{page.off('pageerror',onError);await page.setViewportSize({width:1440,height:1000});}
}
