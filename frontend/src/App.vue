<script setup lang="ts">
import {ref,computed,onMounted,onUnmounted,watch,nextTick,defineAsyncComponent} from 'vue';
import type {Snapshot,WindowDef,Entry,Row,FileEntry,MenuAction} from './types';
import {rpc,DirectoryStore,selectItems,dropIntent,relativePath} from './behavior';
import Icon from './Icon.vue';
import EntryIcon from './EntryIcon.vue';
import TransferFeedback from './TransferFeedback.vue';
const loadSettings=()=>import('./SettingsPanel.vue');
const SettingsPanel=defineAsyncComponent(loadSettings);

const query=new URLSearchParams(location.search),settings=query.has('settings'),pinChooser=query.has('pin');
const prepared=ref(query.has('prepared'));
let requestedId=query.get('window')||(query.get('settings')!=='global'?query.get('settings'):null);
const nativeWindowId=computed(()=>prepared.value?query.get('prepared'):settings?'settings-'+(requestedId||'global'):pinChooser?'pin-chooser':win.value?.id);
const snapshot=ref<Snapshot>(),error=ref(''),busy=ref(false),loading=ref(true);
const focused=ref(document.hasFocus());
const selected=ref(new Set<string>()),anchor=ref(''),group=ref<Entry>(),expanded=ref(new Set<string>()),directoryVersion=ref(0);
const groupEditing=ref(false),groupName=ref(''),groupNameSaving=ref(false),scrollActive=ref(false);
let scrollTimer:ReturnType<typeof setTimeout>;
let scrollPointerInside=false;
const scrollTop=ref(0),viewport=ref<HTMLElement>(),viewportHeight=ref(400),history=ref<string[]>([]),historyIndex=ref(-1);
const menu=ref<{request:number,anchorX:number,anchorY:number,x:number,y:number,row?:Row,context:'blank'|'file'|'folder'|'group',actions:MenuAction[]}>();
let menuRequest=0;
const dialog=ref<{title:string,message?:string,value?:string,confirm:string,danger?:boolean,recycle?:boolean,run:(value:string)=>Promise<void>} >();
const dialogValue=ref(''),dialogError=ref(''),dialogNoMorePrompt=ref(false),dragged=ref(false);
const collapseSaving=ref(false);
const creating=ref<{path:string,directory:boolean,name:string,error:string,saving:boolean}>();
const transferFeedback=ref<{transfer:(paths:string[],destination:string,operation:'copy'|'move')=>Promise<void>,notify:(error:unknown)=>void}>();
const win=computed(()=>snapshot.value?.window),isVirtual=computed(()=>win.value?.type!=='folder');
const directories=new DirectoryStore(async(path,offset,limit)=>rpc('ListDirectory',{windowId:win.value?.id,path,offset,limit}));
const stats=ref(new Map<string,FileEntry>()),iconErrors=ref(new Set<string>());
const scale=computed(()=>snapshot.value?.appearance.scale||1);
const appearanceStyle=computed(()=>{const a=snapshot.value?.appearance;if(!a)return {};return {'--surface':a.backgroundColor,'--title':a.titleColor,'--title-text':a.titleTextColor||a.titleColor,'--folder':a.folderColor,'--text':a.textColor,'--corner-radius':`${settings||pinChooser?7:a.cornerRadius??7}px`,'--scale':String(a.scale),'--row-height':`${Math.round(30*a.scale)}px`};});
const light=computed(()=>snapshot.value?.global.theme==='light'||snapshot.value?.global.theme==='system'&&!snapshot.value?.systemDark);
let snapshotBusy=false,statKey='',timer:ReturnType<typeof setInterval>,refreshTimer:ReturnType<typeof setInterval>,resize:ResizeObserver;
function applySnapshot(next:Snapshot){
  // A native URL always identifies its own window; never silently switch to another saved window.
  if(next.window&&snapshot.value?.window?.id===next.window.id&&next.window.revision<snapshot.value.window.revision)next.window=snapshot.value.window;
  snapshot.value=next;
  if(group.value){group.value=next.window?.items.find(e=>e.id===group.value?.id&&e.type==='group');if(!group.value)groupEditing.value=false;}
  if(next.window && !expanded.value.size)expanded.value=new Set(next.window.expandedPaths||[]);
  if(next.window?.type==='folder'&&historyIndex.value<0){history.value=[next.window.currentPath];historyIndex.value=0;}
  const paths=allEntries(next.window?.items||[]).filter(e=>e.path).map(e=>e.path!);const key=JSON.stringify(paths);
  if(key!==statKey){statKey=key;if(paths.length)rpc<FileEntry[]>('StatPaths',{paths}).then(v=>{if(key===statKey)stats.value=new Map(v.map(f=>[f.path,f]));}).catch(showError);}
}
async function refresh(){
 if(snapshotBusy)return;snapshotBusy=true;
 try{
  const next=await rpc<Snapshot>('Snapshot',requestedId?{windowId:requestedId}:{});
  applySnapshot(next);
 }catch(e){showError(e);}finally{loading.value=false;snapshotBusy=false;}
}
function showError(e:unknown){const message=e instanceof Error?e.message:String(e);if(/cancelled by user|canceled by user|用户取消/i.test(message)||error.value===message)return;error.value=message;console.warn(message);void rpc('ReportError',{text:message}).catch(console.error);}
async function act(method:string,params:Record<string,unknown>={}){
 try{busy.value=true;error.value='';const result=await rpc<any>(method,params);const updated=result?.window||result;if(updated?.id===win.value?.id&&updated?.revision!==undefined&&snapshot.value)snapshot.value={...snapshot.value,window:updated};await refresh();return result;}
 catch(e){showError(e);throw e;}finally{busy.value=false;}
}
async function safe(method:string,params:Record<string,unknown>={}){try{return await act(method,params);}catch{return undefined;}}
async function update(patch:Partial<WindowDef>){if(win.value)await safe('UpdateWindow',{windowId:win.value.id,patch});}
function allEntries(entries:Entry[]):Entry[]{return entries.flatMap(e=>[e,...(e.items||[])]);}
function fileIconURL(path:string){return '/api/icon?path='+encodeURIComponent(path)+'&v=6';}
function entryRow(e:Entry,depth=0):Row{
 const stat=e.path?stats.value.get(e.path):undefined;
 return {id:e.id,name:e.displayName||e.name||stat?.name||e.path?.split(/[\\/]/).filter(Boolean).pop()||'未命名',path:e.path,kind:e.type==='group'?'group':stat?.isDir?'folder':'file',depth,entry:e,error:stat?.error,icon:stat?.icon||(e.path?fileIconURL(e.path):undefined)};
}
function fileRow(f:FileEntry,depth:number,system=false):Row{return {id:f.path,name:f.name,path:f.path,kind:f.isDir?'folder':'file',depth,file:f,system,error:f.error,icon:f.icon||fileIconURL(f.path)};}
function entryLabel(row:Row){if(!grid.value||win.value?.showFileExtensions||row.kind!=='file'||row.entry?.displayName)return row.name;const dot=row.name.lastIndexOf('.');return dot>0&&dot<row.name.length-1?row.name.slice(0,dot):row.name;}
function children(path:string,depth:number,system=false):Row[]{
 directoryVersion.value;const cache=directories.cache.get(path);
 const out:Row[]=[];
 if(creating.value&&relativePath(path,creating.value.path)==='.')out.push({id:'deskgrid-inline-create',name:creating.value.name,kind:creating.value.directory?'folder':'file',depth,creating:true});
 if(!cache)return out;
 for(const f of cache.entries){const row=fileRow(f,depth,system);out.push(row);if(f.isDir&&expanded.value.has(f.path)&&!f.isLink)out.push(...children(f.path,depth+1,system));}
 return out;
}
const rows=computed(()=>{
 const w=win.value;if(!w)return [];
 if(w.type==='folder')return children(w.currentPath,0);
 const out:Row[]=[];
 if(w.type==='start')for(const d of snapshot.value?.drives||[]){out.push({id:d.path,name:d.name,path:d.path,kind:'drive',depth:0,system:true,error:d.error});if(w.layout==='list'&&expanded.value.has(d.path))out.push(...children(d.path,1,true));}
 for(const e of w.items){out.push(entryRow(e));if(w.layout==='list'&&e.type==='group'&&expanded.value.has(e.id))out.push(...(e.items||[]).map(x=>entryRow(x,1)));}
 return out;
});
const groupRows=computed(()=>(win.value?.items.find(e=>e.id===group.value?.id&&e.type==='group')?.items||[]).map(e=>entryRow(e)));
const grid=computed(()=>win.value?.layout==='grid'&&isVirtual.value);
const activeRows=computed(()=>group.value&&grid.value?groupRows.value:rows.value);
const selectable=computed(()=>activeRows.value.filter(r=>!r.system&&!r.creating));
const selection=computed(()=>selectable.value.filter(r=>selected.value.has(r.id)));
const selectedPaths=computed(()=>selection.value.filter(r=>r.path&&!r.error).map(r=>r.path!));
const rowHeight=computed(()=>Math.round(30*scale.value));
const virtualized=computed(()=>!grid.value&&rows.value.length>150);
const rangeStart=computed(()=>virtualized.value?Math.max(0,Math.floor(scrollTop.value/rowHeight.value)-12):0);
const visibleRows=computed(()=>virtualized.value?rows.value.slice(rangeStart.value,rangeStart.value+Math.ceil(viewportHeight.value/rowHeight.value)+24):rows.value);
const currentCache=computed(()=>{directoryVersion.value;return directories.cache.get(win.value?.currentPath||'');});
const canUp=computed(()=>!!win.value&&relativePath(win.value.rootPath,win.value.currentPath)!=='.');
async function loadDirectory(path:string,append=false){const pending=directories.load(path,append);directoryVersion.value++;await pending;directoryVersion.value++;}
async function refreshDirectories(){if(!win.value||settings||pinChooser)return;const paths=win.value.type==='folder'?[win.value.currentPath,...expanded.value]:[...expanded.value];await Promise.all([...new Set(paths)].filter(p=>p.includes('\\')||p.includes('/')).map(p=>loadDirectory(p)));if(isVirtual.value){const pinned=allEntries(win.value.items).filter(e=>e.path).map(e=>e.path!);if(pinned.length){const key=JSON.stringify(pinned);try{const result=await rpc<FileEntry[]>('StatPaths',{paths:pinned});if(key===statKey)stats.value=new Map(result.map(f=>[f.path,f]));}catch(e){showError(e);}}}}
watch(()=>win.value?.currentPath,(path,old)=>{if(path&&win.value?.type==='folder'&&path!==old){creating.value=undefined;selected.value=new Set();scrollTop.value=0;loadDirectory(path);}});
watch(()=>activeRows.value.map(r=>r.id).join('\n'),()=>{selected.value=new Set([...selected.value].filter(id=>activeRows.value.some(r=>r.id===id)));});
watch(grid,value=>{if(!value)group.value=undefined;});
async function toggle(row:Row){
 if(row.file?.isLink){showError('链接目录不会自动展开，请双击打开。');return;}
 const next=new Set(expanded.value),opening=!next.has(row.id);if(opening)next.add(row.id);else next.delete(row.id);
 expanded.value=next;await update({expandedPaths:[...next]});
 if(opening&&row.path&&!directories.cache.has(row.path))await loadDirectory(row.path);
}
async function navigate(path:string,record=true){
 if(!win.value)return;
 try{await act('Navigate',{windowId:win.value.id,path});if(record){history.value=history.value.slice(0,historyIndex.value+1);history.value.push(path);historyIndex.value++;}group.value=undefined;selected.value=new Set();await loadDirectory(path);}catch{}
}
async function goHistory(delta:number){const index=historyIndex.value+delta;if(index>=0&&index<history.value.length){await navigate(history.value[index],false);if(win.value?.currentPath===history.value[index])historyIndex.value=index;}}
function parentPath(path:string){return path.replace(/[\\/]+$/,'').replace(/[^\\/]+$/,'')||path;}
async function open(row:Row){
 if(row.error){showError(row.error);return;}
 if(row.kind==='group'){if(grid.value)group.value=row.entry;else await toggle(row);return;}
 if(!row.path)return;
 if(row.kind==='folder'||row.kind==='drive'){if(win.value?.type==='folder')await navigate(row.path);else await safe('CreateWindow',{type:'folder',path:row.path});}
 else await safe('OpenPath',{path:row.path});
}
function select(row:Row,e:MouseEvent){if(row.system)return;selected.value=selectItems(selectable.value.map(r=>r.id),row.id,selected.value,anchor.value,{ctrl:e.ctrlKey||e.metaKey,shift:e.shiftKey});if(!e.shiftKey)anchor.value=row.id;}
let lastRowClick:{id:string,time:number,x:number,y:number}|undefined;
function clickRow(row:Row,e:MouseEvent){
 if(row.creating||performance.now()<clickSuppressedUntil||fileDragging)return;
 if(win.value?.type!=='folder'&&e.detail>1&&!e.ctrlKey&&!e.metaKey&&!e.shiftKey)return;
 select(row,e);
 if(win.value?.type==='folder'){
  if(e.ctrlKey||e.metaKey||e.shiftKey){lastRowClick=undefined;return;}
  const now=performance.now(),previous=lastRowClick;
  if(previous?.id===row.id&&now-previous.time<=250&&Math.hypot(e.clientX-previous.x,e.clientY-previous.y)<=4){lastRowClick=undefined;void open(row);return;}
  lastRowClick={id:row.id,time:now,x:e.clientX,y:e.clientY};
 }else if(!e.ctrlKey&&!e.metaKey&&!e.shiftKey)void open(row);
}
function ask(title:string,confirm:string,run:(value:string)=>Promise<void>,value?:string,message?:string,danger=false,recycle=false,selectStem=false){dialog.value={title,confirm,run,value,message,danger,recycle};dialogValue.value=value||'';dialogError.value='';dialogNoMorePrompt.value=false;menu.value=undefined;nextTick(()=>{const input=document.querySelector<HTMLInputElement>('.dialog input:not([type=checkbox])');if(!input)return;input.focus();const dot=input.value.lastIndexOf('.');input.setSelectionRange(0,selectStem&&dot>0&&dot<input.value.length-1?dot:input.value.length);});}
async function submitDialog(){if(!dialog.value)return;try{dialogError.value='';busy.value=true;await dialog.value.run(dialogValue.value);dialog.value=undefined;}catch(e){dialogError.value=(e as Error).message;}finally{busy.value=false;}}
function rename(row=selection.value[0]){if(!row||row.system)return;ask(row.entry?'编辑显示名称':'重命名真实文件','保存',async name=>{if(!name.trim())throw new Error('名称不能为空');if(row.entry)await act('MutateEntries',{windowId:win.value!.id,operation:'rename',ids:[row.id],name});else await act('RenameFile',{windowId:win.value!.id,path:row.path,name});await refreshDirectories();},row.name,undefined,false,false,!row.entry&&row.kind==='file');}
watch(()=>group.value?.id,()=>{groupEditing.value=false;selected.value=new Set();anchor.value='';lastRowClick=undefined;});
function editGroupName(){if(!group.value)return;groupName.value=group.value.name||'';groupEditing.value=true;nextTick(()=>{const input=document.querySelector<HTMLInputElement>('.group-title-input');input?.focus();input?.select();});}
function cancelGroupName(){groupEditing.value=false;groupName.value=group.value?.name||'';}
async function saveGroupName(){
 if(!group.value||!groupEditing.value||groupNameSaving.value)return;
 const name=groupName.value.trim(),id=group.value.id;
 if(!name||name===group.value.name){cancelGroupName();return;}
 groupNameSaving.value=true;
 try{await act('MutateEntries',{windowId:win.value!.id,operation:'rename',ids:[id],name});if(group.value?.id===id){group.value={...group.value,name};groupEditing.value=false;}}
 catch{}finally{groupNameSaving.value=false;}
}
function hideScrollActivity(){clearTimeout(scrollTimer);scrollActive.value=false;}
function showScrollActivity(){scrollActive.value=true;clearTimeout(scrollTimer);scrollTimer=setTimeout(()=>scrollActive.value=false,800);}
function scrollPointerActivity(){scrollPointerInside=true;showScrollActivity();}
function scrollPointerLeft(){scrollPointerInside=false;hideScrollActivity();}
function contentScrolled(e:Event){scrollTop.value=(e.target as HTMLElement).scrollTop;if(focused.value||scrollPointerInside)showScrollActivity();}
function relocate(row:Row){ask('重新定位入口','保存',async path=>{const stat=await rpc<FileEntry[]>('StatPaths',{paths:[path]});if(!stat[0]||stat[0].error)throw new Error(stat[0]?.error||'目标不存在');const items=win.value!.items.map(e=>({...e,path:e.id===row.id?path:e.path,items:e.items?.map(item=>item.id===row.id?{...item,path}:item)}));await act('UpdateWindow',{windowId:win.value!.id,patch:{items}});statKey='';await refresh();},row.path||'','输入真实文件或文件夹的完整路径。只修改入口指向，不移动目标。');}
async function remove(){
 const items=selection.value;if(!items.length)return;
 if(isVirtual.value){const result=await safe('MutateEntries',{windowId:win.value!.id,operation:'remove',ids:items.map(r=>r.id)});if(result)selected.value=new Set();return;}
 const windowId=win.value!.id,paths=[...selectedPaths.value];
 const recycle=async()=>{await act('Recycle',{windowId,paths});await refreshDirectories();selected.value=new Set();};
 if(win.value?.skipRecycleConfirmation){await recycle();return;}
 ask('移到回收站','确认',async()=>{if(dialogNoMorePrompt.value)await act('UpdateWindow',{windowId,patch:{skipRecycleConfirmation:true}});await recycle();},undefined,`将 ${items.length} 项文件移到 Windows 回收站。`,true,true);
}
async function clipboard(cut=false){if(cut&&isVirtual.value){showError('虚拟入口请通过拖动或“移入应用文件夹”整理，不剪切真实目标。');return;}if(selection.value.some(r=>r.kind==='group')){showError('请展开分组并选择具体入口。');return;}if(selectedPaths.value.length)await safe('ClipboardWrite',{paths:selectedPaths.value,cut});}
async function paste(path=win.value?.currentPath){await safe('Paste',{windowId:win.value!.id,path,conflict:'keepBoth'});await refreshDirectories();}
async function focusNewEntry(){await nextTick();const index=rows.value.findIndex(row=>row.creating);if(virtualized.value&&index>=0&&viewport.value){viewport.value.scrollTop=index*rowHeight.value;scrollTop.value=viewport.value.scrollTop;await nextTick();}const input=document.querySelector<HTMLInputElement>('.inline-create input');input?.scrollIntoView({block:'nearest'});input?.focus();}
async function createItem(directory:boolean,path=win.value?.currentPath){
 if(!path||win.value?.type!=='folder'||creating.value?.saving)return;
 creating.value={path,directory,name:'',error:'',saving:false};selected.value=new Set();
 if(relativePath(win.value.currentPath,path)!=='.'&&!expanded.value.has(path)){expanded.value=new Set([...expanded.value,path]);await update({expandedPaths:[...expanded.value]});await loadDirectory(path);}
 await focusNewEntry();
}
function cancelNewEntry(){if(!creating.value?.saving)creating.value=undefined;}
function newEntryKeydown(e:KeyboardEvent){
 e.stopPropagation();
 if(e.isComposing||e.keyCode===229)return;
 if(e.key==='Enter'){e.preventDefault();void commitNewEntry();}
 else if(e.key==='Escape'){e.preventDefault();cancelNewEntry();}
}
async function commitNewEntry(blur=false){
 const draft=creating.value;if(!draft||draft.saving)return;const name=draft.name.trim();
 if(!name){if(blur)cancelNewEntry();else draft.error='请输入名称';return;}
 draft.saving=true;draft.error='';
 try{await act('CreateFile',{windowId:win.value!.id,path:draft.path,name,directory:draft.directory});if(creating.value===draft)creating.value=undefined;await refreshDirectories();const item=rows.value.find(row=>row.name===name&&row.path&&relativePath(draft.path,parentPath(row.path))==='.');if(item){selected.value=new Set([item.id]);anchor.value=item.id;}}
 catch(error){draft.error=error instanceof Error?error.message:String(error);draft.saving=false;if(creating.value===draft)await focusNewEntry();}
 finally{draft.saving=false;}
}
async function createWindow(type:'collection'|'folder'){
 if(type==='folder'){const path=await safe('ChooseFolder');if(path)await safe('CreateWindow',{type,path});}
 else ask('新建应用窗口','创建',async title=>{await act('CreateWindow',{type,title});},'应用窗口');
}
async function pinPaths(paths:string[],windowId:string,groupId?:string,targetEntryId?:string,insertIndex?:number){const result=await act('Pin',{windowId,paths,groupId,targetEntryId,insertIndex});if(result?.duplicates?.length){const existing=allEntries(win.value?.items||[]).filter(e=>result.duplicates.includes(e.path));selected.value=new Set(existing.map(e=>e.id));}}
async function choosePin(paths=selectedPaths.value){ask('固定到窗口','固定',async id=>{let target=id;if(id==='new'){const w=await act('CreateWindow',{type:'collection',title:'应用窗口'});target=w.id;}await pinPaths(paths,target);},snapshot.value?.windows.find(w=>w.type==='collection')?.id||'new','pin-target');}
function groupEntries(){const ids=selection.value.map(r=>r.id);if(!ids.length||selection.value.some(r=>r.kind==='group'))return;ask('创建应用文件夹','创建',async name=>{await act('MutateEntries',{windowId:win.value!.id,operation:'group',ids,name});},'应用文件夹');}
async function reorder(delta:number){const row=selection.value[0];if(!row)return;const entries=group.value?.items||win.value!.items;const index=Math.max(0,Math.min(entries.length-1,entries.findIndex(e=>e.id===row.id)+delta));await safe('MutateEntries',{windowId:win.value!.id,operation:'reorder',ids:[row.id],index});}
async function settingsOpen(context='blank'){await safe('Settings',{windowId:win.value?.id,context});}
async function keyboard(e:KeyboardEvent){
 if((e.target as HTMLElement).closest('input,textarea,select,[contenteditable=true]'))return;
 showScrollActivity();
 if(win.value?.type==='folder'&&e.altKey&&['ArrowLeft','ArrowRight','ArrowUp'].includes(e.key)){e.preventDefault();if(e.key==='ArrowLeft')await goHistory(-1);else if(e.key==='ArrowRight')await goHistory(1);else if(canUp.value)await navigate(parentPath(win.value.currentPath));return;}
 if(e.key==='F5'){e.preventDefault();await refreshDirectories();return;}
 if(e.key==='Escape'){finishDrag();menu.value=undefined;dialog.value=undefined;group.value=undefined;selected.value=new Set();return;}
 const ctrl=e.ctrlKey||e.metaKey;

 if(ctrl&&['a','c','x','v','d','+','=','-','0'].includes(e.key.toLowerCase()))e.preventDefault();
 if(ctrl&&e.key.toLowerCase()==='a'){
  if(win.value?.type==='folder'){for(const path of [win.value.currentPath,...expanded.value]){let page=directories.cache.get(path);while(page&&page.entries.length<page.total&&!page.error){await loadDirectory(path,true);const next=directories.cache.get(path);if(!next||next.entries.length<=page.entries.length)break;page=next;}}}
  selected.value=new Set(selectable.value.map(r=>r.id));
 }
 else if(ctrl&&e.key.toLowerCase()==='c')await clipboard();
 else if(ctrl&&e.key.toLowerCase()==='x')await clipboard(true);
 else if(ctrl&&e.key.toLowerCase()==='v')await paste();
 else if(e.key==='Delete'||ctrl&&e.key.toLowerCase()==='d')remove();
 else if(e.key==='F2'){e.preventDefault();if(selection.value.length===1)rename();}
 else if(e.key==='Enter'&&selection.value.length===1)await open(selection.value[0]);
 else if(ctrl&&['+','=','-','0'].includes(e.key)){const value=e.key==='0'?1:Math.max(.75,Math.min(2,Math.round((scale.value+(e.key==='-'?-.1:.1))*100)/100));await update({appearanceOverrides:{...win.value!.appearanceOverrides,scale:value}});}
 else if(['ArrowDown','ArrowUp','ArrowLeft','ArrowRight'].includes(e.key)){
  e.preventDefault();const ids=selectable.value.map(r=>r.id);const current=ids.indexOf([...selected.value].at(-1)||'');const step=e.key==='ArrowDown'||e.key==='ArrowRight'?1:-1;const next=ids[Math.max(0,Math.min(ids.length-1,current+step))];if(next){selected.value=selectItems(ids,next,selected.value,anchor.value,{shift:e.shiftKey,ctrl:false});if(!e.shiftKey)anchor.value=next;const index=activeRows.value.findIndex(r=>r.id===next);if(viewport.value&&virtualized.value&&!group.value)viewport.value.scrollTop=index*rowHeight.value;else nextTick(()=>document.querySelector<HTMLElement>(`${group.value?'.group-panel':'.content'} [data-row-id="${CSS.escape(next)}"]`)?.scrollIntoView({block:'nearest'}));}
 }
}
function contextFor(row?:Row):'blank'|'folder'|'file'|'group'{return !row?'blank':row.kind==='group'?'group':row.kind==='folder'||row.kind==='drive'?'folder':'file';}
async function contextMenu(e:MouseEvent,row?:Row){
 e.preventDefault();e.stopPropagation();if(!win.value)return;
 if(row?.creating)return;
 if(win.value.collapsed){dismissMenu();return;}
 lastRowClick=undefined;
 if(row&&!row.system&&!selected.value.has(row.id)){selected.value=new Set([row.id]);anchor.value=row.id;}
 if(!row)selected.value=new Set();
 const context=contextFor(row),request=++menuRequest;menu.value=undefined;
 try{
  const actions=await rpc<MenuAction[]>('GetMenu',{windowId:win.value.id,context});if(menuRequest!==request)return;
  menu.value={request,anchorX:e.clientX,anchorY:e.clientY,x:e.clientX,y:e.clientY,row,context,actions};
  const available=localActions.value;
  const selected=await rpc<string>('ShowContextMenu',{windowId:win.value.id,x:e.clientX,y:e.clientY,actions:available.map(a=>({id:a.id,name:a.name,enabled:a.enabled,group:a.group}))});
  const action=available.find(a=>a.id===selected);if(menuRequest===request)menu.value=undefined;action?.run();
 }catch(e){showError(e);}
}
type LocalAction={id:string,name:string,enabled:boolean,run:()=>unknown,danger?:boolean,group?:string};
const localActions=computed(()=>{
 const m=menu.value,w=win.value;if(!m||!w)return [];
 const row=m.row,real=w.type==='folder',one=selection.value.length===1,single=!row||one,path=row?.path||w.currentPath;
 const actions=new Map<string,LocalAction>();
 const add=(id:string,name:string,enabled:boolean,run:()=>unknown,danger=false)=>actions.set(id,{id,name,enabled,run,danger});
 add('settings','设置',true,()=>settingsOpen(m.context));
 if(row?.system){
  add('open','打开',!row.error,()=>open(row));
  add('refresh','刷新',true,async()=>{await refresh();await refreshDirectories();});
  return ['open','refresh','settings'].map(id=>({...actions.get(id)!,group:id==='settings'?'settings':'actions'}));
 }
 if(row){
  add(row.kind==='group'?'openGroup':'open','打开',one&&!row.error,()=>open(row));
  add('rename',real?'重命名':'编辑名称',one,()=>rename(row));
  add(real?'recycle':'remove',real?'删除':'取消固定',selection.value.length>0,remove,true);
  if(row.error&&row.entry){add('relocate','重新定位入口…',one,()=>relocate(row));return ['relocate','remove','settings'].map(id=>({...actions.get(id)!,group:id==='settings'?'settings':'actions'}));}
  if(real){add('cut','剪切',selectedPaths.value.length>0,()=>clipboard(true));add('copy','复制',selectedPaths.value.length>0,()=>clipboard());}
  else{
   add('moveFirst','移到前面',one,()=>reorder(-100000));add('movePrevious','向前移动',one,()=>reorder(-1));add('moveNext','向后移动',one,()=>reorder(1));
   add('createGroup','创建应用文件夹',selection.value.length>0&&!selection.value.some(r=>r.kind==='group'),groupEntries);
   const groups=w.items.filter(e=>e.type==='group');
   add('moveToGroup',group.value?'移出应用文件夹':'移入应用文件夹',selection.value.every(r=>r.kind!=='group')&&(!!group.value||groups.length>0),()=>{
    if(group.value)return safe('MutateEntries',{windowId:w.id,operation:'moveToGroup',ids:selection.value.map(r=>r.id)});
    ask('移入应用文件夹','移动',async groupId=>{await act('MutateEntries',{windowId:w.id,operation:'moveToGroup',ids:selection.value.map(r=>r.id),groupId});},groups[0]?.id,'group-target');
   });
   add('ungroup','解散应用文件夹',one,()=>safe('MutateEntries',{windowId:w.id,operation:'ungroup',ids:[row.id]}));
  }
  if(row.path){
   add('pin','固定到窗口',selectedPaths.value.length>0,()=>choosePin());
   add('reveal','在文件资源管理器中显示',one&&!row.error,()=>safe('OpenPath',{path:row.path,verb:'reveal'}));
   if(row.kind==='file')add('openWith','打开方式',one&&!row.error,()=>safe('OpenPath',{path:row.path,verb:'openas'}));
   if(/\.(exe|lnk|bat|cmd)$/i.test(row.path))add('runas','以管理员身份运行',one&&!row.error,()=>safe('OpenPath',{path:row.path,verb:'runas'}));
  }
 }else if(!real){
  add('createCollection','新建应用窗口',true,()=>createWindow('collection'));
  if(w.type!=='start')add('createFolder','打开文件夹窗口…',true,()=>createWindow('folder'));
  add('renameWindow','重命名窗口',true,()=>ask('重命名窗口','保存',async title=>{await act('UpdateWindow',{windowId:w.id,patch:{title,titleMode:'custom'}});},w.title));
  add('deleteWindow','删除窗口',!w.positionLocked,()=>safe('DeleteWindow',{windowId:w.id}),true);
  add('toggleLayout',w.layout==='grid'?'切换列表':'切换大图标',true,()=>update({layout:w.layout==='grid'?'list':'grid'}));
 }
 if(real&&(!row||row.kind==='folder'&&selection.value.every(r=>r.kind==='folder'))){
  add('newFile','新建文件',single,()=>createItem(false,path));add('newDirectory','新建文件夹',single,()=>createItem(true,path));
  add('pin',row?'固定到窗口':'固定当前目录到窗口',!row||selectedPaths.value.length>0,()=>choosePin(row?selectedPaths.value:[path]));
  add('openNewWindow','在新窗口打开',single,()=>safe('CreateWindow',{type:'folder',path}));
  add('reveal','在文件资源管理器中显示',single,()=>safe('OpenPath',{path,verb:'reveal'}));
  add('cmd','在 cmd 中打开',single,()=>safe('OpenPath',{path,verb:'cmd'}));add('powershell','在 PowerShell 中打开',single,()=>safe('OpenPath',{path,verb:'powershell'}));
  add('paste','粘贴',single,()=>paste(path));
 }
 if(real&&path){
  const targets=row?selectedPaths.value:[path];
  add('copyPath','复制路径',targets.length>0,()=>safe('ClipboardText',{text:targets.join('\r\n')}));
  add('copyRelativePath','复制相对路径',targets.length>0&&targets.every(p=>relativePath(w.rootPath,p)!==null),()=>safe('ClipboardText',{text:targets.map(p=>relativePath(w.rootPath,p)).join('\r\n')}));
 }
 return m.actions.flatMap(spec=>{
  if(!spec.enabled)return [];
  if(spec.custom)return [{id:spec.id,name:spec.name,group:spec.group||'custom',enabled:!!path||selectedPaths.value.length>0,run:()=>safe('RunCommand',{windowId:w.id,context:m.context,actionId:spec.id,paths:selectedPaths.value.length?selectedPaths.value:path?[path]:[]})}];
  const action=actions.get(spec.id);return action?[{...action,group:spec.group,name:spec.id==='toggleLayout'||spec.id==='moveToGroup'?action.name:spec.name}]:[];
 });
});
function dismissMenu(){menuRequest++;menu.value=undefined;}
function pointerActivity(e:PointerEvent){
 const target=e.target instanceof Element?e.target:undefined;
 if(target?.closest('.context-menu'))return;
 dismissMenu();
 if(!target?.closest('[data-row-id]')||target.closest('button'))lastRowClick=undefined;
 if(win.value)void rpc('WindowInteracted',{windowId:win.value.id}).catch(console.error);
}
function scrollActivity(e:Event){if(e.target instanceof Element&&e.target.closest('.context-menu'))return;dismissMenu();lastRowClick=undefined;}
function keyActivity(e:KeyboardEvent){if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='z'&&win.value&&!(e.target instanceof Element&&e.target.closest('input,textarea,select,[contenteditable=true]'))){e.preventDefault();e.stopPropagation();void safe('UndoTransfer').then(refreshDirectories);return;}if(e.key==='Escape'||!(e.target instanceof Element&&e.target.closest('.context-menu')))dismissMenu();lastRowClick=undefined;}
function closeMenus(e:MouseEvent){if(!(e.target as HTMLElement).closest('.context-menu,.group-panel,.group-card')){dismissMenu();if(group.value&&!(e.target as HTMLElement).closest('.dialog'))group.value=undefined;}}
let pointer:{x:number,y:number,title:boolean,moved:boolean}|undefined;
function pointerDown(e:PointerEvent,title=false){if(e.button!==0||(e.target as HTMLElement).closest('button,input,select,a,[draggable=true]'))return;pointer={x:e.clientX,y:e.clientY,title,moved:false};}
async function pointerMove(e:PointerEvent){if(!pointer||pointer.moved)return;if(Math.hypot(e.clientX-pointer.x,e.clientY-pointer.y)>5){pointer.moved=true;dragged.value=true;if(settings||pinChooser||!win.value?.positionLocked)await safe('NativeDragStart',{windowId:nativeWindowId.value});}}
function dividerHeight(){const title=document.querySelector<HTMLElement>('.titlebar'),surface=document.querySelector<HTMLElement>('.deskgrid');return title&&surface?title.getBoundingClientRect().bottom-surface.getBoundingClientRect().top:33;}
async function toggleCollapsed(){if(!win.value||win.value.positionLocked||collapseSaving.value)return;dismissMenu();group.value=undefined;collapseSaving.value=true;try{await update({collapsed:!win.value.collapsed,collapsedHeight:dividerHeight()});}finally{collapseSaving.value=false;}}
function pointerUp(){const clickTitle=pointer?.title&&!pointer.moved;pointer=undefined;if(clickTitle)void toggleCollapsed();setTimeout(()=>dragged.value=false,0);}
type InternalDrag={windowId:string,ids:string[],paths:string[],virtual:boolean,group:boolean,sourceGroupId?:string};
let fileDragging=false,clickSuppressedUntil=0;
let hoverKey='',hoverTimer:ReturnType<typeof setTimeout>;
function clearDragHover(){clearTimeout(hoverTimer);hoverKey='';}
function finishDrag(){clearDragHover();if(fileDragging){clickSuppressedUntil=performance.now()+300;fileDragging=false;}}
function dragLeft(e:DragEvent){if(e.relatedTarget instanceof Node&&document.documentElement.contains(e.relatedTarget))return;if(!e.relatedTarget||e.clientX<=0||e.clientY<=0||e.clientX>=innerWidth||e.clientY>=innerHeight)clearDragHover();}
function dropped(){queueMicrotask(finishDrag);}
function escapeDrag(e:KeyboardEvent){if(e.key==='Escape')finishDrag();}
function nativeDragLeft(){clearDragHover();}
function startDrag(e:DragEvent,row:Row){
 dismissMenu();lastRowClick=undefined;
 finishDrag();
 if(row.system||row.error||row.creating){e.preventDefault();return;}
 fileDragging=true;dragged.value=true;
 if(!selected.value.has(row.id))selected.value=new Set([row.id]);
 const payload:InternalDrag={windowId:win.value!.id,ids:[...selected.value],paths:selectedPaths.value,virtual:isVirtual.value,group:selection.value.some(r=>r.kind==='group'),sourceGroupId:group.value?.id};
 e.dataTransfer!.effectAllowed='copyMove';e.dataTransfer!.setData('application/x-deskgrid',JSON.stringify(payload));
}
function dragOver(e:DragEvent,row?:Row){
 if(row?.creating){e.preventDefault();e.stopPropagation();return;}
 if(!row&&(e.target as Element).closest('.content [data-row-id]'))return;
 const internal=e.dataTransfer?.types.includes('application/x-deskgrid');if(!internal&&!e.dataTransfer?.types.includes('Files'))return;
 e.preventDefault();if(internal)e.stopPropagation();
 const key=internal&&!isVirtual.value&&row?.kind==='folder'&&!selected.value.has(row.id)&&!expanded.value.has(row.id)&&!row.file?.isLink?row.id:'';
 if(key!==hoverKey){clearDragHover();hoverKey=key;if(key&&row)hoverTimer=setTimeout(()=>void toggle(row),500);}
}
function dropGroupId(entryId?:string){if(!entryId)return;return win.value?.items.find(item=>item.type==='group'&&(item.id===entryId||item.items?.some(child=>child.id===entryId)))?.id;}
function virtualDropTarget(x:number,y:number,target:Element|null,row?:Row):{groupId?:string,targetEntryId?:string,index:number}{
 const w=win.value!,panel=target?.closest<HTMLElement>('.group-panel');
 if(panel?.dataset.groupId)return {groupId:panel.dataset.groupId,index:w.items.length};
 if(row?.entry&&!w.items.some(item=>item.id===row.id))return {groupId:dropGroupId(row.id),index:w.items.length};
 const entries=[...(viewport.value?.querySelectorAll<HTMLElement>('[data-entry-id]')||[])].map(element=>({id:element.dataset.entryId!,rect:element.getBoundingClientRect(),index:w.items.findIndex(item=>item.id===element.dataset.entryId)})).filter(item=>item.index>=0);
 const hit=entries.find(item=>item.id===row?.id)||entries.find(({rect})=>x>=rect.left&&x<=rect.right&&y>=rect.top&&y<=rect.bottom);
 if(hit){
  const {rect}=hit,rx=(x-rect.left)/rect.width,ry=(y-rect.top)/rect.height;
  const center=grid.value?rx>=.25&&rx<=.75&&ry>=.15&&ry<=.85:ry>=.25&&ry<=.75;
  if(center&&!target?.classList.contains('group-backdrop')){const item=w.items[hit.index];return item.type==='group'?{groupId:item.id,index:hit.index}:{targetEntryId:item.id,index:hit.index};}
  const before=grid.value?x<rect.left+rect.width/2:y<rect.top+rect.height/2;
  return {index:hit.index+(before?0:1)};
 }
 // Gaps between tiles belong to the following tile's insertion position.
 const line=entries.filter(({rect})=>y>=rect.top&&y<=rect.bottom);
 if(line.length){const next=line.find(({rect})=>x<rect.left+rect.width/2);return {index:next?.index??line[line.length-1].index+1};}
 return {index:entries.find(({rect})=>y<rect.top+rect.height/2)?.index??w.items.length};
}
async function internalDrop(e:DragEvent,row?:Row){try{if(row?.creating){e.preventDefault();e.stopPropagation();throw new Error('请先完成新建项目。');}await performInternalDrop(e,row);}catch(error){transferFeedback.value?.notify(error);showError(error);}}
async function performInternalDrop(e:DragEvent,row?:Row){
 const raw=e.dataTransfer?.getData('application/x-deskgrid');if(!raw)return;
 e.preventDefault();e.stopPropagation();finishDrag();
 let data:InternalDrag;try{data=JSON.parse(raw);}catch{return;}
 if(!Array.isArray(data.ids)||!data.windowId)return;
 const w=win.value!;
 if(w.type==='folder'){
  if(data.group)throw new Error('整组不能拖到真实目录；请展开并选择具体入口。');
  await transferDrop(data.paths,row?.kind==='folder'?row.path!:w.currentPath,data.virtual?'virtual':'real',e.ctrlKey,e.shiftKey);return;
 }
 const {groupId,targetEntryId,index}=virtualDropTarget(e.clientX,e.clientY,e.target instanceof Element?e.target:null,row);
 if(!data.virtual){await pinPaths(data.paths,w.id,groupId,targetEntryId,index);return;}
 if(data.windowId===w.id){
  if(row?.id&&data.ids.includes(row.id))return;
  if(groupId){if(data.group)throw new Error('不支持分组嵌套。');await act('MutateEntries',{windowId:w.id,operation:'moveToGroup',ids:data.ids,groupId});}
  else if(targetEntryId&&!data.group){const index=w.items.slice(0,w.items.findIndex(e=>e.id===targetEntryId)).filter(e=>!data.ids.includes(e.id)).length;await act('MutateEntries',{windowId:w.id,operation:'group',ids:[targetEntryId,...data.ids],index});}
  else {const insertIndex=w.items.slice(0,index).filter(item=>!data.ids.includes(item.id)).length;const result=await act('MutateEntries',{windowId:w.id,operation:'reorder',ids:data.ids,index:insertIndex});if(result&&data.sourceGroupId&&group.value?.id===data.sourceGroupId)group.value=undefined;}
 }else await act('TransferEntries',{sourceWindowId:data.windowId,targetWindowId:w.id,ids:data.ids,copy:e.ctrlKey,groupId,targetEntryId:data.group?undefined:targetEntryId,index});
}
async function transferDrop(paths:string[],destination:string,source:'real'|'virtual',ctrl:boolean,shift:boolean){
 if(!paths.length)return;const intent=dropIntent(win.value!.type,source,ctrl,shift);
 await transferFeedback.value?.transfer(paths,destination,intent==='copy'?'copy':'move');
}
async function nativeDrop(e:Event){
 const detail=(e as CustomEvent).detail?.data||(e as CustomEvent).detail;if(!detail||detail.windowId!==win.value?.id)return;finishDrag();
 const attrs=detail.attributes||{},hit=document.elementFromPoint(detail.x,detail.y),el=hit?.closest<HTMLElement>('[data-file-drop-target]');
 if(hit?.closest('.inline-create')){transferFeedback.value?.notify('请先完成新建项目。');return;}
 const path=attrs['data-directory']||el?.dataset.directory||win.value!.currentPath;
 const entryId=attrs['data-entry-id']||el?.dataset.entryId;
 const placement=virtualDropTarget(detail.x,detail.y,hit,rows.value.find(row=>row.id===entryId));
 if(attrs['data-group-id']){placement.groupId=attrs['data-group-id'];placement.targetEntryId=undefined;}
 const {groupId,targetEntryId,index}=placement;
 try{if(isVirtual.value)await pinPaths(detail.paths,win.value!.id,groupId,targetEntryId,index);else await transferDrop(detail.paths,path,'real',!!detail.ctrl,!!detail.shift);}catch(e){transferFeedback.value?.notify(e);showError(e);}
}
function formatSize(n:number){return n>=1073741824?`${(n/1073741824).toFixed(1)} GB`:`${Math.round(n/1048576)} MB`;}
function nativeError(e:Event){const detail=(e as CustomEvent).detail;showError(detail?.message||detail?.data?.message||'原生系统操作未完成');}
function focusChanged(e:Event){const wasFocused=focused.value;focused.value=e.type==='focus'||(e as CustomEvent).detail===true;if(!focused.value){scrollPointerInside=false;hideScrollActivity();dismissMenu();lastRowClick=undefined;}else if(!wasFocused)void nativeChanged();}
function focusLost(){focused.value=false;scrollPointerInside=false;hideScrollActivity();dismissMenu();lastRowClick=undefined;}
let lastPixelRatio=0;
function surfaceScaleChanged(){const ratio=window.devicePixelRatio||1;if(lastPixelRatio&&ratio!==lastPixelRatio){lastPixelRatio=ratio;void rpc('WindowPainted',{windowId:nativeWindowId.value,pixelRatio:ratio}).catch(console.error);}}
async function painted(){await nextTick();await new Promise<void>(resolve=>{const fallback=setTimeout(resolve,16);requestAnimationFrame(()=>requestAnimationFrame(()=>{clearTimeout(fallback);resolve();}));});lastPixelRatio=window.devicePixelRatio||1;await rpc('WindowPainted',{windowId:nativeWindowId.value,pixelRatio:lastPixelRatio});}
let contentRefreshing=false,contentRefreshAgain=false;
async function nativeChanged(){
 if(contentRefreshing){contentRefreshAgain=true;return;}
 contentRefreshing=true;
 try{do{contentRefreshAgain=false;await refreshDirectories();await refresh();}while(contentRefreshAgain);}finally{contentRefreshing=false;}
}
async function initializeWindow(boot?:Snapshot){
 document.addEventListener('pointerdown',pointerActivity,true);document.addEventListener('scroll',scrollActivity,true);window.addEventListener('wheel',scrollActivity,true);window.addEventListener('keydown',keyActivity,true);window.addEventListener('resize',dismissMenu);window.addEventListener('deskgrid:dismiss-menu',dismissMenu);
 window.addEventListener('focus',focusChanged);window.addEventListener('blur',focusLost);window.addEventListener('deskgrid:focus',focusChanged);
 window.addEventListener('dragend',finishDrag);window.addEventListener('drop',dropped,true);window.addEventListener('dragleave',dragLeft);window.addEventListener('blur',clearDragHover);window.addEventListener('keydown',escapeDrag);window.addEventListener('deskgrid:drag-leave',nativeDragLeft);
 if(boot){applySnapshot(boot);loading.value=false;}else await refresh();if(settings)await loadSettings();await nextTick();
 if(win.value?.collapsed&&!settings&&!pinChooser&&win.value.collapsedHeight!==dividerHeight())await update({collapsedHeight:dividerHeight()});
 timer=setInterval(refresh,1500);refreshTimer=setInterval(nativeChanged,6000);
 window.addEventListener('deskgrid:changed',nativeChanged);window.addEventListener('deskgrid:drop',nativeDrop);window.addEventListener('deskgrid:error',nativeError);window.addEventListener('pointermove',pointerMove);window.addEventListener('pointerup',pointerUp);window.addEventListener('resize',surfaceScaleChanged);document.addEventListener('click',closeMenus);
 resize=new ResizeObserver(()=>viewportHeight.value=viewport.value?.clientHeight||400);if(viewport.value)resize.observe(viewport.value);
 await painted();
 void refreshDirectories();
}
function assignPrepared(e:Event){if(!prepared.value)return;const data=(e as CustomEvent).detail;if(!data?.windowId||!data.snapshot)return;requestedId=data.windowId;delete (window as any).__deskgridPendingAssignment;window.history.replaceState(null,'','/?window='+encodeURIComponent(data.windowId));prepared.value=false;void initializeWindow(data.snapshot);}
onMounted(async()=>{if(prepared.value){window.addEventListener('deskgrid:assign',assignPrepared);const pending=(window as any).__deskgridPendingAssignment;if(pending)assignPrepared(new CustomEvent('deskgrid:assign',{detail:pending}));else await painted();}else await initializeWindow();});
onUnmounted(()=>{window.removeEventListener('deskgrid:assign',assignPrepared);document.removeEventListener('pointerdown',pointerActivity,true);document.removeEventListener('scroll',scrollActivity,true);window.removeEventListener('wheel',scrollActivity,true);window.removeEventListener('keydown',keyActivity,true);window.removeEventListener('resize',dismissMenu);window.removeEventListener('deskgrid:dismiss-menu',dismissMenu);window.removeEventListener('focus',focusChanged);window.removeEventListener('blur',focusLost);window.removeEventListener('deskgrid:focus',focusChanged);clearDragHover();window.removeEventListener('dragend',finishDrag);window.removeEventListener('drop',dropped,true);window.removeEventListener('dragleave',dragLeft);window.removeEventListener('blur',clearDragHover);window.removeEventListener('keydown',escapeDrag);window.removeEventListener('deskgrid:drag-leave',nativeDragLeft);clearTimeout(scrollTimer);clearInterval(timer);clearInterval(refreshTimer);resize?.disconnect();window.removeEventListener('deskgrid:changed',nativeChanged);window.removeEventListener('deskgrid:drop',nativeDrop);window.removeEventListener('deskgrid:error',nativeError);window.removeEventListener('pointermove',pointerMove);window.removeEventListener('pointerup',pointerUp);window.removeEventListener('resize',surfaceScaleChanged);document.removeEventListener('click',closeMenus);});
</script>

<template>
 <main class="deskgrid" :class="{light,focused,'scroll-active':scrollActive,'reduced-motion':snapshot?.global.reducedMotion,collapsed:win?.collapsed,'icons-only':win?.type==='collection'&&win.iconsOnly&&!settings&&!pinChooser}" :style="appearanceStyle" @pointerenter="scrollPointerActivity" @pointermove="scrollPointerActivity" @pointerleave="scrollPointerLeft" @wheel.passive="showScrollActivity" @keydown.capture="showScrollActivity">
  <template v-if="loading"></template>
  <template v-else-if="settings&&snapshot"><SettingsPanel :snapshot="snapshot" :window-id="requestedId||undefined" @refresh="refresh" @error="showError" @close="safe('CloseWindow',{windowId:nativeWindowId})" @drag="pointerDown($event)"/></template>
  <template v-else-if="pinChooser&&snapshot">
   <header class="titlebar" @pointerdown="pointerDown($event)"><Icon name="pin"/><span class="window-title">固定到 DeskGrid</span><button title="关闭选择器" aria-label="关闭选择器" @click="safe('CloseWindow',{windowId:nativeWindowId})"><Icon name="x" :size="15"/></button></header>
   <section class="chooser"><h2>选择应用窗口</h2><p class="muted">{{snapshot.pendingPins?.length||0}} 个入口 · 原始文件保持原位</p>
    <button v-for="w in snapshot.windows.filter(w=>w.type==='collection')" :key="w.id" class="chooser-window" @click="safe('FinishPin',{windowId:w.id})"><Icon name="grid"/><span>{{w.title}}</span><Icon name="right"/></button>
    <button class="primary" @click="async()=>{const w=await safe('CreateWindow',{type:'collection',title:'应用窗口'});if(w)await safe('FinishPin',{windowId:w.id});}"><Icon name="plus"/>新建应用窗口并固定</button>
   </section>
  </template>
  <template v-else-if="win&&snapshot">
   <header class="titlebar" @pointerdown="pointerDown($event,true)" @contextmenu="contextMenu($event)">
    <Icon :name="win.type==='start'?'monitor':win.type==='folder'?'folder':'grid'" :size="14"/><span class="window-title" :title="win.title">{{win.title}}</span>
    <div class="title-actions"><button v-if="win.type==='folder'" title="返回上一级目录" aria-label="返回上一级目录" :disabled="!canUp" @click.stop="canUp&&navigate(parentPath(win.currentPath))"><Icon name="back" :size="13"/></button><button :class="{active:win.positionLocked}" :title="win.positionLocked?'取消固定':'固定窗口位置和大小'" :aria-label="win.positionLocked?'取消固定':'固定窗口位置和大小'" @click.stop="update({positionLocked:!win.positionLocked})"><Icon :name="win.positionLocked?'pin':'pinOff'" :size="13"/></button><button class="close" :title="win.type==='start'?'起始窗口不能关闭':win.positionLocked?'已锁定，请先解锁再关闭':'关闭并删除窗口'" aria-label="关闭窗口" :disabled="win.type==='start'||win.positionLocked" @click.stop="win.type!=='start'&&!win.positionLocked&&safe('CloseWindow',{windowId:win.id})"><Icon name="x" :size="15"/></button></div>
   </header>
    <section ref="viewport" data-file-drop-target class="content" :class="{grid,list:!grid}" :inert="win.collapsed" :aria-hidden="win.collapsed" tabindex="0" aria-label="文件与入口" @keydown="keyboard" @scroll="contentScrolled" @pointerdown.self="pointerDown($event)" @click.self="!dragged&&(selected=new Set())" @contextmenu.self="contextMenu($event)" @dragover.prevent="dragOver($event)" @drop="internalDrop($event)">
     <div v-if="virtualized" :style="{height:`${rangeStart*rowHeight}px`}"/>
     <div v-for="row in visibleRows" :key="row.id" :data-row-id="row.id" :data-entry-id="row.entry?.id" data-file-drop-target :data-directory="row.kind==='folder'||row.kind==='drive'?row.path:undefined" class="entry" :class="{selected:selected.has(row.id),system:row.system,'group-card':row.kind==='group',missing:row.error,'inline-create':row.creating}" :style="!grid?{paddingLeft:`${8+row.depth*17}px`}:{}" :title="row.error?`${row.path}\n${row.error}`:row.path||row.name" :aria-label="row.name" :draggable="!row.system&&!row.error&&!row.creating" @click.stop="clickRow(row,$event)" @dblclick.stop.prevent @contextmenu="contextMenu($event,row)" @dragstart="startDrag($event,row)" @dragover="dragOver($event,row)" @drop="internalDrop($event,row)">
      <button v-if="!row.creating&&!grid&&['drive','folder','group'].includes(row.kind)" class="tree-toggle" :aria-label="expanded.has(row.id)?'收起 '+row.name:'展开 '+row.name" :aria-expanded="expanded.has(row.id)" @click.stop="toggle(row)"><Icon :name="expanded.has(row.id)?'down':'right'" :size="12"/></button><span v-else-if="!grid" class="tree-spacer"/>
      <div v-if="row.kind==='group'&&grid" class="group-icon"><EntryIcon v-for="item in row.entry?.items?.slice(0,4)" :key="item.id" :row="entryRow(item)" :size="Math.round(12*scale)" :icon-errors="iconErrors" @icon-error="iconErrors.add($event)"/><Icon v-if="!row.entry?.items?.length" name="group" :size="25"/></div>
      <EntryIcon v-else :row="row" :size="grid?Math.round(36*scale):Math.round(18*scale)" :icon-errors="iconErrors" @icon-error="iconErrors.add($event)"/>
      <input v-if="row.creating&&creating" v-model="creating.name" :disabled="creating.saving" :placeholder="creating.directory?'输入文件夹名称':'输入文件名称'" :aria-label="creating.directory?'新建文件夹名称':'新建文件名称'" @click.stop @contextmenu.stop @keydown="newEntryKeydown" @blur="commitNewEntry(true)"/><span v-else class="entry-name">{{entryLabel(row)}}</span><span v-if="row.creating&&creating?.error" class="inline-create-error" role="alert">{{creating.error}}</span><Icon v-if="row.error" name="error" class="error-icon" :size="13"/><Icon v-if="row.file?.isLink" name="link" :size="11"/>
      <span v-if="!grid&&row.kind==='drive'" class="entry-meta">{{formatSize(snapshot.drives?.find(d=>d.path===row.path)?.free||0)}} 可用</span>
     </div>
     <div v-if="virtualized" :style="{height:`${Math.max(0,rows.length-rangeStart-visibleRows.length)*rowHeight}px`}"/>
     <div v-if="currentCache?.error" class="empty-state"><Icon name="error" :size="27"/><strong>无法读取此目录</strong><p>{{currentCache.error}}</p><button @click="loadDirectory(win.currentPath)">重试</button></div>
     <div v-else-if="!rows.length&&!currentCache?.loading" class="empty-state"><Icon :name="isVirtual?'grid':'folder'" :size="32"/><strong>{{isVirtual?'把常用的，放在手边':'此文件夹为空'}}</strong><p>{{isVirtual?'从资源管理器拖入文件、文件夹或程序。':'新建项目，或粘贴剪贴板中的文件。'}}</p><button v-if="isVirtual" @click="async()=>{const path=await safe('ChooseFolder');if(path)await pinPaths([path],win!.id);}"><Icon name="plus"/>固定文件夹…</button><button v-else @click="createItem(true)"><Icon name="plus"/>新建文件夹</button></div>
     <template v-for="[path,cache] in directories.cache" :key="path"><button v-if="cache.entries.length<cache.total&&(path===win.currentPath||expanded.has(path))" class="load-more" :disabled="cache.loading" @click="loadDirectory(path,true)">{{`加载更多 · ${cache.entries.length} / ${cache.total}`}}</button><div v-if="cache.error&&expanded.has(path)&&path!==win.currentPath" class="inline-error">{{path}}：{{cache.error}} <button @click="loadDirectory(path)">重试</button></div></template>
    </section>
  </template>
  <div v-else-if="!settings&&!pinChooser" class="empty-state"><Icon name="error" :size="32"/><strong>窗口不可用</strong><p>当前原生窗口没有对应的保存定义。</p><button @click="refresh">重试</button></div>

  <div v-if="group&&grid" class="group-backdrop" @click.self="group=undefined" @dragover="dragOver($event)" @drop="internalDrop($event)">
   <section class="group-panel" data-file-drop-target :data-group-id="group.id" tabindex="0" @keydown="keyboard" @dragover="dragOver($event)" @drop="internalDrop($event)">
    <header><input v-if="groupEditing" v-model="groupName" class="group-title-input" aria-label="应用文件夹名称" :disabled="groupNameSaving" @blur="saveGroupName" @keydown.enter.prevent.stop="saveGroupName" @keydown.esc.prevent.stop="cancelGroupName"/><button v-else class="group-title" title="点击修改名称" @click.stop="editGroupName">{{group.name}}</button><button title="收起分组" @click="group=undefined"><Icon name="x" :size="15"/></button></header>
    <div class="group-grid"><div v-for="row in groupRows" :key="row.id" :data-row-id="row.id" class="entry" :class="{selected:selected.has(row.id)}" :draggable="!row.error" :title="row.path" @click.stop="clickRow(row,$event)" @dblclick.stop.prevent @contextmenu="contextMenu($event,row)" @dragstart="startDrag($event,row)"><EntryIcon :row="row" :size="Math.round(32*scale)" :icon-errors="iconErrors" @icon-error="iconErrors.add($event)"/><span class="group-entry-name">{{entryLabel(row)}}</span></div></div>
   </section>
  </div>

  <TransferFeedback v-if="win&&!settings&&!pinChooser" ref="transferFeedback" :window-id="win.id" @changed="refreshDirectories" @error="showError"/>
  <div v-if="dialog" class="modal-backdrop" @keydown.esc="!busy&&(dialog=undefined)"><form class="dialog" @submit.prevent="submitDialog"><header><h2>{{dialog.title}}</h2><button type="button" aria-label="关闭对话框" :disabled="busy" @click="dialog=undefined"><Icon name="x"/></button></header><p v-if="dialog.message&&dialog.message!=='pin-target'&&dialog.message!=='group-target'">{{dialog.message}}</p><select v-if="dialog.message==='pin-target'" v-model="dialogValue" aria-label="目标窗口"><option v-for="w in snapshot?.windows.filter(w=>w.type==='collection')" :key="w.id" :value="w.id">{{w.title}}</option><option value="new">新建应用窗口并固定</option></select><select v-else-if="dialog.message==='group-target'" v-model="dialogValue" aria-label="目标应用文件夹"><option v-for="g in win?.items.filter(e=>e.type==='group')" :key="g.id" :value="g.id">{{g.name}}</option></select><input v-else-if="dialog.value!==undefined" v-model="dialogValue" :aria-label="dialog.title" autofocus/><p v-if="dialogError" class="inline-error" role="alert">{{dialogError}}</p><footer><label v-if="dialog.recycle" class="dialog-suppress"><input v-model="dialogNoMorePrompt" type="checkbox" :disabled="busy"/>不再提示</label><button type="button" :disabled="busy" @click="dialog=undefined">取消</button><button type="submit" :class="dialog.danger?'danger-button':'primary'" :disabled="busy">{{busy?'处理中…':dialog.confirm}}</button></footer></form></div>
 </main>
</template>
<style scoped>
.group-title-input{flex:1;min-width:0;max-width:calc(100% - 32px);min-height:28px;font-weight:600}
</style>
