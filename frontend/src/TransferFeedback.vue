<script setup lang="ts">
import {ref,watch,nextTick,onUnmounted} from 'vue';
import {rpc} from './behavior';
import Icon from './Icon.vue';
const props=defineProps<{windowId:string}>();
const emit=defineEmits<{changed:[],error:[unknown]}>();
type Request={paths:string[],destination:string,operation:'copy'|'move'};
type State={id:string,status:string,error?:string};
const pending=ref<Request&{conflicts:string[]}>(),busy=ref(false);
const cancelButton=ref<HTMLButtonElement>();
watch(pending,async request=>{if(request){await nextTick();cancelButton.value?.focus();}});
const toasts=ref<{id:number,message:string}[]>([]);
const tasks=new Set<string>(),timers=new Set<ReturnType<typeof setTimeout>>();
let pollTimer:ReturnType<typeof setInterval>|undefined,polling=false,sequence=0,disposed=false;
function notify(error:unknown){
 const message=error instanceof Error?error.message:String(error),id=++sequence;
 toasts.value=[...toasts.value.slice(-2),{id,message}];
 const timer=setTimeout(()=>{toasts.value=toasts.value.filter(t=>t.id!==id);timers.delete(timer);},6500);timers.add(timer);
}
function fail(error:unknown){notify(error);emit('error',error);}
async function poll(){
 if(polling||!tasks.size||disposed)return;polling=true;
 try{
  const states=await rpc<State[]>('GetTransferTasks',{ids:[...tasks]});
  if(disposed)return;
  for(const task of states){
   if(!['completed','failed','cancelled'].includes(task.status))continue;
   tasks.delete(task.id);emit('changed');
   if(task.status==='failed')fail(new Error(task.error||'文件拖动失败，请检查目标文件夹或文件占用状态。'));
  }
  if(!tasks.size){clearInterval(pollTimer);pollTimer=undefined;}
 }catch(error){if(!disposed){fail(error);clearInterval(pollTimer);pollTimer=undefined;tasks.clear();}}
 finally{polling=false;}
}
async function start(request:Request,conflict:'replace'|'keepBoth'){
 const task=await rpc<State>('TransferFiles',{windowId:props.windowId,paths:request.paths,destination:request.destination,operation:request.operation,conflict});
 if(disposed)return;
 tasks.add(task.id);if(!pollTimer)pollTimer=setInterval(poll,450);void poll();
}
async function transfer(paths:string[],destination:string,operation:'copy'|'move'){
 if(!paths.length)return;
 if(pending.value||busy.value){notify('请先处理当前的文件替换确认。');return;}
 busy.value=true;
 try{
  const request={paths:[...paths],destination,operation};
  const conflicts=await rpc<string[]>('CheckTransferConflicts',{windowId:props.windowId,...request});
  if(disposed)return;
  if(conflicts.length)pending.value={...request,conflicts};else await start(request,'keepBoth');
 }catch(error){if(!disposed)fail(error);}finally{busy.value=false;}
}
async function replace(){
 if(!pending.value||busy.value)return;busy.value=true;
 const request=pending.value;
 try{await start(request,'replace');pending.value=undefined;}
 catch(error){fail(error);pending.value=undefined;}finally{busy.value=false;}
}
function cancel(){if(!busy.value)pending.value=undefined;}
onUnmounted(()=>{disposed=true;clearInterval(pollTimer);for(const timer of timers)clearTimeout(timer);});
defineExpose({transfer,notify});
</script>
<template>
 <div v-if="pending" class="modal-backdrop" @keydown.esc.prevent.stop="cancel">
  <section class="dialog transfer-confirm" role="dialog" aria-modal="true" aria-labelledby="transfer-title">
   <header><h2 id="transfer-title">文件已存在</h2><button title="取消" aria-label="取消替换" :disabled="busy" @click="cancel"><Icon name="x" :size="15"/></button></header>
   <p>目标位置已存在同名文件或文件夹，是否替换？</p>
   <ul><li v-for="path in pending.conflicts.slice(0,3)" :key="path" :title="path">{{path.split(/[\\/]/).pop()}}</li></ul>
   <p v-if="pending.conflicts.length>3">另有 {{pending.conflicts.length-3}} 项。</p>
   <p class="transfer-note">同名文件将被覆盖，文件夹将合并。</p>
   <footer><button ref="cancelButton" :disabled="busy" @click="cancel">取消</button><button class="danger-button" :disabled="busy" @click="replace">{{busy?'处理中…':'替换'}}</button></footer>
  </section>
 </div>
 <div class="transfer-toasts" aria-live="polite">
  <div v-for="toast in toasts" :key="toast.id" class="transfer-toast" role="alert"><Icon name="error" :size="16"/><span>{{toast.message}}</span><button aria-label="关闭失败提示" @click="toasts=toasts.filter(t=>t.id!==toast.id)"><Icon name="x" :size="13"/></button></div>
 </div>
</template>
<style scoped>
.transfer-confirm{max-height:calc(100dvh - 30px);overflow:auto}.transfer-confirm ul{margin:10px 0;padding-left:19px;font-size:12px;max-height:100px;overflow:auto}.transfer-confirm li{overflow-wrap:anywhere;margin:5px 0}.transfer-note{font-size:10px!important}.transfer-toasts{position:absolute;right:12px;bottom:12px;left:12px;z-index:110;display:flex;flex-direction:column;gap:8px;pointer-events:none}.transfer-toast{display:flex;align-items:flex-start;gap:8px;padding:11px;background:var(--surface);border:1px solid #d27c6070;border-radius:8px;box-shadow:0 5px 20px #0004;color:var(--text);font-size:11px;pointer-events:auto;animation:toast-in .16s ease-out}.transfer-toast>svg{color:#e5a28c;flex-shrink:0}.transfer-toast>span{flex:1;min-width:0;max-height:min(100px,calc(100dvh - 70px));overflow:auto;overflow-wrap:anywhere}.transfer-toast button{min-height:16px;padding:0;flex-shrink:0}@keyframes toast-in{from{opacity:0;transform:translateY(5px)}to{opacity:1;transform:translateY(0)}}
</style>
