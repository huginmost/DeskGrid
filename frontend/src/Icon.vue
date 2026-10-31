<script setup lang="ts">
import {Folder,File,Image as ImageIcon,HardDrive,FolderOpen,ChevronRight,ChevronDown,Pin,PinOff,X,Settings,Plus,ArrowLeft,ArrowRight,ArrowUp,RefreshCw,MoreHorizontal,LayoutGrid,List,Search,Check,Copy,Trash2,ExternalLink,Link,Layers,Monitor,AlertCircle,Command,GripVertical} from 'lucide-vue-next';
const props=defineProps<{name:string,size?:number}>();
const icons:Record<string,any>={folder:Folder,file:File,image:ImageIcon,drive:HardDrive,open:FolderOpen,right:ChevronRight,down:ChevronDown,pin:Pin,pinOff:PinOff,x:X,settings:Settings,plus:Plus,back:ArrowLeft,forward:ArrowRight,up:ArrowUp,refresh:RefreshCw,more:MoreHorizontal,grid:LayoutGrid,list:List,search:Search,check:Check,copy:Copy,trash:Trash2,external:ExternalLink,link:Link,group:Layers,monitor:Monitor,error:AlertCircle,command:Command,grip:GripVertical};
</script>
<template><component :is="icons[props.name]||File" :class="{'folder-icon':name==='folder'||name==='open'}" :size="size||16" :stroke-width="1.6" aria-hidden="true"/></template>

