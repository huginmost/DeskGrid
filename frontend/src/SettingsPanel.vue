<script setup lang="ts">
import {ref,computed,watch,onMounted,onUnmounted} from 'vue';
import type {Snapshot,Appearance,Global,MenuAction,MenuPatch} from './types';
import {rpc} from './behavior';
import Icon from './Icon.vue';
const props=defineProps<{snapshot:Snapshot,windowId?:string}>();
const emit=defineEmits<{refresh:[],error:[unknown],close:[],drag:[PointerEvent]}>();
const tab=ref('窗口'),target=ref(props.windowId||''),error=ref(''),saving=ref(false);
const recordingPeek=ref('');
const recorderWindowId='settings-'+(props.windowId||'global');
const availableWindows=computed(()=>props.snapshot.windows.filter(w=>props.snapshot.openWindowIds.includes(w.id)));
const current=computed(()=>availableWindows.value.find(w=>w.id===target.value));
const appearance=ref<Appearance>({...props.snapshot.appearance});
const overrides=ref<Partial<Appearance>>({...current.value?.appearanceOverrides});
const context=ref(new URLSearchParams(location.search).get('context')||'blank'),editGlobalMenu=ref(false),actions=ref<MenuAction[]>([]),menuLoading=ref(false);
const command=ref<{id:string,name:string,mode:'program'|'cmd'|'powershell',template:string}>(),preview=ref<any[]>([]),confirmReset=ref(false),dragIndex=ref(-1);
let saveTimer:ReturnType<typeof setTimeout>,menuRequest=0,appearanceRequest=0;
watch(()=>availableWindows.value.map(w=>w.id).join(','),()=>{if(target.value&&!availableWindows.value.some(w=>w.id===target.value)){target.value='';actions.value=[];menuRequest++;}});
watch(()=>props.snapshot.appearance,async a=>{if(saving.value)return;if(!target.value||target.value===props.windowId){appearance.value={...a};return;}const request=++appearanceRequest;try{const result=await rpc<Snapshot>('Snapshot',{windowId:target.value});if(request===appearanceRequest&&!saving.value)appearance.value={...result.appearance};}catch(e){fail(e);}});
watch(target,async()=>{clearTimeout(saveTimer);overrides.value={...current.value?.appearanceOverrides};const request=++appearanceRequest;if(target.value)try{const result=await rpc<Snapshot>('Snapshot',{windowId:target.value});if(request===appearanceRequest)appearance.value={...result.appearance};}catch(e){fail(e);}loadMenu();});
function fail(e:unknown){error.value=(e as Error).message||String(e);emit('error',e);}
async function run(method:string,params:Record<string,unknown>={}){try{error.value='';saving.value=true;const r=await rpc<any>(method,params);emit('refresh');return r;}catch(e){fail(e);throw e;}finally{saving.value=false;}}
async function safe(method:string,params:Record<string,unknown>={}){try{return await run(method,params);}catch{return undefined;}}
function changeAppearance(field:keyof Appearance,value:string|number){
 if(typeof value==='string'&&!/^#[0-9a-f]{6}$/i.test(value)){error.value='颜色必须为 #RRGGBB 格式';return;}
 error.value='';appearance.value={...appearance.value,[field]:value};overrides.value={...overrides.value,[field]:value};saving.value=true;
 clearTimeout(saveTimer);saveTimer=setTimeout(async()=>{await safe('UpdateWindow',{windowId:current.value!.id,patch:{appearanceOverrides:{...overrides.value}}});},300);
}
function inherit(field:keyof Appearance){const next={...overrides.value};delete next[field];overrides.value=next;safe('UpdateWindow',{windowId:current.value!.id,patch:{appearanceOverrides:next}});}
async function globalChange(field:keyof Global,value:unknown){await safe('SaveGlobal',{patch:{[field]:value}});}
async function loadMenu(){const request=++menuRequest;if(!current.value){actions.value=[];menuLoading.value=false;return;}menuLoading.value=true;try{const result=await rpc<MenuAction[]>('GetMenu',{windowId:current.value.id,context:context.value,global:editGlobalMenu.value});if(request===menuRequest)actions.value=result||[];}catch(e){fail(e);}finally{if(request===menuRequest)menuLoading.value=false;}}
watch([context,tab,editGlobalMenu],()=>{if(tab.value==='菜单')loadMenu();});
async function saveMenu(patch:MenuPatch){await run('SaveMenu',{windowId:current.value!.id,context:context.value,patch,global:editGlobalMenu.value});await loadMenu();}
async function toggleAction(action:MenuAction){await saveMenu({actions:{[action.id]:{enabled:!action.enabled}}});}
async function moveAction(index:number,delta:number){const next=[...actions.value];const [item]=next.splice(index,1);next.splice(Math.max(0,Math.min(next.length,index+delta)),0,item);await saveMenu({order:next.map(a=>a.id)});}
async function dropAction(index:number){if(dragIndex.value<0)return;await moveAction(dragIndex.value,index-dragIndex.value);dragIndex.value=-1;}
function addCommand(index:number){command.value={id:'custom-'+crypto.randomUUID(),name:'',mode:'program',template:''};preview.value=[];insertAfter.value=index;}
const insertAfter=ref(-1);
async function commandPreview(){if(!command.value)return;try{preview.value=await run('PreviewCommand',{template:command.value.template,mode:command.value.mode,paths:[current.value?.currentPath||current.value?.rootPath||'C:\\示例 文件.txt'],root:current.value?.rootPath||''});}catch{preview.value=[];}}
async function saveCommand(){if(!command.value?.name.trim()||!command.value.template.trim()){error.value='请输入名称和命令模板';return;}const draft={...command.value};await commandPreview();if(!preview.value.length||command.value?.id!==draft.id)return;const order=actions.value.map(a=>a.id);if(!order.includes(draft.id))order.splice(insertAfter.value+1,0,draft.id);try{await saveMenu({order,actions:{[draft.id]:{...draft,custom:true,enabled:true}}});if(command.value?.id===draft.id)command.value=undefined;}catch{}}
async function deleteCommand(a:MenuAction){await saveMenu({actions:{[a.id]:{enabled:false,template:'',name:a.name}},order:actions.value.filter(x=>x.id!==a.id).map(x=>x.id)});}
function recordHotkey(e:KeyboardEvent){e.preventDefault();const key=e.key;if(['Control','Shift','Alt','Meta'].includes(key))return;const parts=[e.ctrlKey?'Ctrl':'',e.altKey?'Alt':'',e.shiftKey?'Shift':'',e.metaKey?'Win':'',key.length===1?key.toUpperCase():key].filter(Boolean);globalChange('hotkey',parts.join('+'));}
async function stopPeekRecording(){const requestId=recordingPeek.value;recordingPeek.value='';if(requestId)await rpc('EndPeekRecording',{windowId:recorderWindowId,requestId}).catch(()=>{});}
async function startPeekRecording(){
 if(recordingPeek.value){await stopPeekRecording();return;}
 const requestId=crypto.randomUUID();recordingPeek.value=requestId;error.value='';
 try{await rpc('BeginPeekRecording',{windowId:recorderWindowId,requestId});if(recordingPeek.value!==requestId)await rpc('EndPeekRecording',{windowId:recorderWindowId,requestId});}
 catch(e){if(recordingPeek.value===requestId)recordingPeek.value='';fail(e);}
}
function peekRecorded(event:Event){const detail=(event as CustomEvent).detail;if(!detail||detail.requestId!==recordingPeek.value)return;recordingPeek.value='';if(detail.cancelled)return;if(detail.error){fail(new Error(detail.error));return;}if(typeof detail.binding==='string')void globalChange('peekHotkey',detail.binding);}
async function setPeekHotkey(binding:string){await stopPeekRecording();await globalChange('peekHotkey',binding);}
watch(tab,value=>{if(value!=='全局')void stopPeekRecording();});
onMounted(()=>{window.addEventListener('deskgrid:peek-recorded',peekRecorded);window.addEventListener('blur',stopPeekRecording);});
onUnmounted(()=>{clearTimeout(saveTimer);void stopPeekRecording();window.removeEventListener('deskgrid:peek-recorded',peekRecorded);window.removeEventListener('blur',stopPeekRecording);});
</script>
<template>
 <section class="settings-window">
  <header class="settings-header" @pointerdown="emit('drag',$event)"><div><Icon name="settings" :size="18"/><strong>设置</strong></div><button title="关闭设置" aria-label="关闭设置" @click="emit('close')"><Icon name="x" :size="15"/></button></header>
  <nav class="settings-tabs" aria-label="设置类别"><button v-for="name in ['窗口','菜单','全局']" :key="name" :class="{active:tab===name}" @click="tab=name">{{name}}</button></nav>
  <div class="settings-body">
   <div v-if="tab!=='全局'" class="settings-target"><span>正在设置</span><select v-model="target" aria-label="正在设置的窗口"><option value="" disabled>请选择窗口</option><option v-for="w in availableWindows" :key="w.id" :value="w.id">{{w.title}}</option></select></div>
   <p v-if="error" class="inline-error" role="alert">{{error}}</p>
   <template v-if="tab==='窗口'">
    <template v-if="current">
     <h3>操作与显示</h3>
     <label v-if="current.type==='folder'" class="toggle-row"><span>删除到回收站时显示确认</span><input type="checkbox" :checked="!current.skipRecycleConfirmation" :disabled="saving" @change="safe('UpdateWindow',{windowId:current!.id,patch:{skipRecycleConfirmation:!($event.target as HTMLInputElement).checked}})"/></label>
     <label v-if="current.type!=='folder'" class="toggle-row"><span>大图标显示文件扩展名<small>关闭时仅隐藏显示后缀，不更改文件名称；列表始终显示完整名称。</small></span><input type="checkbox" :checked="!!current.showFileExtensions" :disabled="saving" @change="safe('UpdateWindow',{windowId:current!.id,patch:{showFileExtensions:($event.target as HTMLInputElement).checked}})"/></label>
     <label v-if="current.type==='collection'" class="toggle-row"><span>只显示图标，隐藏文字<small>隐藏此应用窗口及应用文件夹中的项目文字，悬停可查看名称。</small></span><input type="checkbox" :checked="!!current.iconsOnly" :disabled="saving" @change="safe('UpdateWindow',{windowId:current!.id,patch:{iconsOnly:($event.target as HTMLInputElement).checked}})"/></label>
     <h3>外观 <span>逐项继承全局默认</span></h3>
     <div class="setting-block"><label for="corner-radius">窗口圆角 <b>{{Math.round(appearance.cornerRadius??7)}} px</b></label><input id="corner-radius" type="range" min="0" max="24" step="1" :value="appearance.cornerRadius??7" aria-label="窗口圆角" @input="changeAppearance('cornerRadius',Number(($event.target as HTMLInputElement).value))"/><button v-if="overrides.cornerRadius!==undefined" class="inherit-link" @click="inherit('cornerRadius')">恢复继承</button></div>
     <div class="setting-block"><label for="opacity">透明度 <b>{{Math.round(appearance.opacity*100)}}%</b></label><input id="opacity" type="range" min="10" max="100" step="1" :value="Math.round(appearance.opacity*100)" aria-label="透明度" @input="changeAppearance('opacity',Number(($event.target as HTMLInputElement).value)/100)"/><button v-if="overrides.opacity!==undefined" class="inherit-link" @click="inherit('opacity')">恢复继承</button></div>
     <div class="setting-block"><label for="scale">内容缩放 <b>{{Math.round(appearance.scale*100)}}%</b></label><input id="scale" type="range" min="75" max="200" step="5" :value="Math.round(appearance.scale*100)" aria-label="内容缩放" @input="changeAppearance('scale',Number(($event.target as HTMLInputElement).value)/100)"/><button v-if="overrides.scale!==undefined" class="inherit-link" @click="inherit('scale')">恢复继承</button></div>
     <div v-for="(label,field) in {titleColor:'标题栏控件颜色',titleTextColor:'标题字体颜色',backgroundColor:'背景颜色',folderColor:'文件夹图标颜色',textColor:'名称文字颜色'}" :key="field" class="color-setting"><label :for="field">{{label}}<small>{{overrides[field]===undefined?'继承全局':'此窗口覆盖'}}</small></label><input :id="field" type="color" :value="appearance[field]" :aria-label="label+'取色'" @input="changeAppearance(field,($event.target as HTMLInputElement).value)"/><input type="text" :value="appearance[field]" :aria-label="label" maxlength="7" @change="changeAppearance(field,($event.target as HTMLInputElement).value)"/><button :disabled="overrides[field]===undefined" title="恢复此项继承" @click="inherit(field)"><Icon name="refresh" :size="13"/></button></div>
     <div class="appearance-preview" :style="{background:appearance.backgroundColor,color:appearance.textColor,borderRadius:`${appearance.cornerRadius??7}px`}"><span :style="{color:appearance.titleTextColor||appearance.titleColor}">{{current.title}}</span><div><Icon name="folder" :size="Math.round(28*appearance.scale)" :style="{color:appearance.folderColor}"/><span :style="{fontSize:`${12*appearance.scale}px`}">文件夹预览</span></div></div>
     <div class="button-stack"><button @click="safe('ApplyAppearanceGlobal',{windowId:current.id})">应用此外观到全局默认</button><button @click="async()=>{await safe('ResetAppearance',{windowId:current!.id});overrides={};}">恢复此窗口的全局默认</button></div><p class="settings-note">全局默认只影响继承字段，其他窗口的显式设置会保留。</p>
    </template><p v-else class="muted">先选择要设置的窗口。</p>
   </template>
   <template v-else-if="tab==='菜单'">
    <h3>右键菜单 <span>{{current?.type==='folder'?'真实目录':'虚拟入口'}}</span></h3>
    <div class="context-tabs"><button v-for="(label,key) in {blank:'空白区域',folder:'文件夹',file:'文件',...(current?.type!=='folder'?{group:'应用文件夹'}:{})}" :key="key" :class="{active:context===key}" @click="context=key">{{label}}</button></div>
    <label class="toggle-row"><span>编辑此类型与上下文的全局菜单</span><input v-model="editGlobalMenu" type="checkbox"/></label>
    <p class="settings-note">内置动作可以隐藏；顺序和动作字段分别继承。</p>
    <template v-if="!menuLoading"><div v-for="(action,index) in actions" :key="action.id" class="menu-editor-row" draggable="true" @dragstart="dragIndex=index" @dragover.prevent @drop.prevent="dropAction(index)"><Icon name="grip" :size="12"/><span :class="{muted:!action.enabled}">{{action.name}}</span><button :title="action.enabled?'隐藏':'启用'" :aria-label="action.name+(action.enabled?'隐藏':'启用')" class="switch" :class="{on:action.enabled}" @click="toggleAction(action)"><i/></button><button title="向上移动" :disabled="index===0" @click="moveAction(index,-1)"><Icon name="up" :size="12"/></button><button title="插入自定义命令" @click="addCommand(index)"><Icon name="plus" :size="13"/></button><button :title="action.custom?'删除自定义命令':'隐藏内置项'" @click="action.custom?deleteCommand(action):action.enabled&&toggleAction(action)"><Icon name="x" :size="13"/></button></div>
    <button class="wide" @click="addCommand(actions.length-1)"><Icon name="plus" :size="14"/>添加自定义命令</button>
    <div v-if="command" class="command-editor"><h3>自定义命令</h3><label>显示名称<input v-model="command.name" aria-label="命令名称"/></label><label>执行模式<select v-model="command.mode"><option value="program">程序 + 参数</option><option value="cmd">cmd</option><option value="powershell">PowerShell</option></select></label><label>命令模板<textarea v-model="command.template" placeholder='code "%path%"' rows="3"/></label><p class="settings-note">%path% · %fileName% · %name% · %directory% · %root% · %paths%<br/>%paths% 需要独占参数；管道和重定向需选择 shell 模式。</p><button @click="commandPreview">解析预览</button><pre v-for="(item,index) in preview" :key="index">程序：{{item.program}}
参数：{{JSON.stringify(item.args)}}
工作目录：{{item.directory}}</pre><div class="button-row"><button @click="command=undefined">取消</button><button class="primary" :disabled="saving" @click="saveCommand">保存命令</button></div></div>
    <div class="button-stack"><button @click="confirmReset=true">恢复默认 / 清除覆盖…</button></div><div v-if="confirmReset" class="inline-confirm"><p>清除此上下文的覆盖与自定义命令，恢复继承。</p><button @click="confirmReset=false">取消</button><button @click="async()=>{await safe('ResetMenu',{windowId:current!.id,context,global:editGlobalMenu});confirmReset=false;loadMenu();}">确认恢复</button></div></template>
   </template>
   <template v-else>
    <h3>全局偏好</h3><label class="setting-row"><span>主题</span><select :value="snapshot.global.theme" aria-label="主题" @change="globalChange('theme',($event.target as HTMLSelectElement).value)"><option value="system">跟随系统</option><option value="dark">深色</option><option value="light">浅色</option></select></label>
    <label v-for="(label,key) in {autoStart:'开机启动',shellIntegration:'资源管理器“固定到 DeskGrid”',hiddenFiles:'显示隐藏文件',reducedMotion:'减少动画'}" :key="key" class="toggle-row"><span>{{label}}<small v-if="key==='autoStart'">{{snapshot.status.autoStart}}</small><small v-if="key==='shellIntegration'">{{snapshot.status.shellIntegration}}</small></span><input type="checkbox" :checked="snapshot.global[key]" @change="globalChange(key,($event.target as HTMLInputElement).checked)"/></label>
    <label class="toggle-row"><span>隐藏起始窗口<small>保留起始窗口与配置，关闭此项即可恢复显示。</small></span><input type="checkbox" :checked="!!snapshot.global.hideStartWindow" :disabled="saving" @change="globalChange('hideStartWindow',($event.target as HTMLInputElement).checked)"/></label>
    <div class="setting-block"><label>显示位置 <b>仅在桌面显示</b></label><p class="settings-note">{{snapshot.status.desktop}}</p></div>
    <p class="settings-note">主内容窗口平时位于桌面层，临时前置时显示在其他软件上方，收回后返回桌面。桌面重启时自动恢复。Windows 11 的固定入口位于“显示更多选项”。</p>
    <h3>临时前置快捷键</h3>
    <input :value="recordingPeek?'请按下新的组合键…':(snapshot.global.peekHotkey??'Win+A')||'未设置'" aria-label="临时前置快捷键" readonly @click="!recordingPeek&&startPeekRecording()" @keydown.prevent.stop/>
    <div class="button-row"><button :disabled="saving" @click="startPeekRecording">{{recordingPeek?'取消录制':'录制按键'}}</button><button :disabled="saving" @click="setPeekHotkey('')">清除</button><button :disabled="saving" @click="setPeekHotkey('Win+A')">恢复默认</button></div>
    <p class="settings-note">{{snapshot.status.peekHotkey}}。点击输入框或“录制按键”后按组合键，Esc 取消；支持 Ctrl / Alt / Shift / Win 搭配字母、数字、F 键及常用功能键，也可单独使用 F1–F24。默认 Win+A，启用时用于本软件；单按 Win 仍打开开始菜单。再次按组合键、点击窗口外或整组失焦后收回桌面。</p><button class="wide" @click="safe('TogglePeek')">临时前置 / 收回</button>
    <h3>显示 / 隐藏快捷键</h3><input :value="snapshot.global.hotkey||'未设置'" aria-label="全局快捷键" readonly @keydown="recordHotkey"/><p class="settings-note">点击输入框，按下新的组合键。{{snapshot.status.hotkey}}</p><div class="button-row"><button @click="globalChange('hotkey','')">清除</button><button @click="globalChange('hotkey','Ctrl+Alt+S')">恢复默认</button><button @click="safe('ToggleAll')">测试显示 / 隐藏</button></div>
   </template>
  </div>
 </section>
</template>
