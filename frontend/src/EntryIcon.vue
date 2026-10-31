<script setup lang="ts">
import {computed,ref,watch,onMounted,onUnmounted} from 'vue';
import type {Row} from './types';
import Icon from './Icon.vue';
const props=defineProps<{row:Row,size:number,iconErrors:Set<string>}>();
const emit=defineEmits<{'icon-error':[string]}>();
const pixelRatio=ref(window.devicePixelRatio||1);
function updatePixelRatio(){pixelRatio.value=window.devicePixelRatio||1;}
const source=computed(()=>{let icon=props.row.icon;if(props.row.kind!=='file'||!icon||!(icon.startsWith('data:image/')||icon.startsWith('/api/')))return;if(icon.startsWith('/api/icon?')){const url=new URL(icon,location.origin);url.searchParams.set('size',props.size*pixelRatio.value>48?'256':'48');url.searchParams.set('v','6');icon=url.pathname+url.search;}return props.iconErrors.has(icon)?undefined:icon;});
const loaded=ref(false),attempt=ref(0);
let retryTimer:ReturnType<typeof setTimeout>;
const imageSource=computed(()=>source.value&&attempt.value&&source.value.startsWith('/api/')?source.value+(source.value.includes('?')?'&':'?')+'retry='+attempt.value:source.value);
const fallbackName=computed(()=>props.row.kind==='folder'?'folder':props.row.kind==='drive'?'drive':props.row.kind==='group'?'group':/\.(apng|avif|bmp|dib|gif|heic|heif|ico|jfif|jpe?g|png|svg|tiff?|webp)$/i.test(props.row.path||props.row.name)?'image':'file');
watch(source,()=>{clearTimeout(retryTimer);loaded.value=false;attempt.value=0;},{flush:'sync'});
function imageLoaded(event:Event){const image=event.currentTarget as HTMLImageElement;if(image.getAttribute('src')!==imageSource.value)return;if(image.naturalWidth&&image.naturalHeight){clearTimeout(retryTimer);loaded.value=true;}else imageFailed(event);}
function imageFailed(event:Event){
 const url=(event.currentTarget as HTMLImageElement).getAttribute('src');if(url!==imageSource.value||!source.value)return;
 loaded.value=false;clearTimeout(retryTimer);
 if(source.value.startsWith('/api/')&&attempt.value<2){retryTimer=setTimeout(()=>attempt.value++,300*(attempt.value+1));return;}
 emit('icon-error',source.value);
}
onMounted(()=>window.addEventListener('resize',updatePixelRatio));
onUnmounted(()=>{clearTimeout(retryTimer);window.removeEventListener('resize',updatePixelRatio);});
</script>
<template>
 <span class="entry-icon-box" :style="{width:`${size}px`,height:`${size}px`}" aria-hidden="true">
  <Icon v-if="!source||!loaded" :name="fallbackName" class="entry-icon" :class="{folder:row.kind==='folder',drive:row.kind==='drive'}" :size="size"/>
  <img v-if="source" :key="imageSource" :src="imageSource" class="file-icon" :class="{loaded}" :style="{width:`${size}px`,height:`${size}px`}" alt="" loading="lazy" decoding="async" @load="imageLoaded" @error="imageFailed"/>
 </span>
</template>
<style scoped>
.entry-icon-box{display:inline-flex;align-items:center;justify-content:center;position:relative;flex:0 0 auto;vertical-align:middle}
.entry-icon-box>.file-icon{position:absolute;inset:0;opacity:0;max-width:100%;max-height:100%}
.entry-icon-box>.file-icon.loaded{opacity:1}
</style>
