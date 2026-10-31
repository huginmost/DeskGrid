import {createApp} from 'vue';
import App from './App.vue';
import './style.css';
// The Go host serves this exact pinned Wails runtime locally. It installs the
// native file-drop bridge and signals WindowRuntimeReady to the native host.
const nativeRuntime='/wails/runtime.js';
import(/* @vite-ignore */ nativeRuntime).then(()=>createApp(App).mount('#app')).catch((error:unknown)=>{
 const root=document.getElementById('app');if(root)root.textContent=`无法加载本机 Wails 运行时，请从 DeskGrid.exe 启动。${String(error)}`;
});
