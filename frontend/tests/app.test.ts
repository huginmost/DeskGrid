import {it,expect,vi,afterEach} from 'vitest';
import {mount,flushPromises} from '@vue/test-utils';
import App from '../src/App.vue';
const windowDef={schemaVersion:1,id:'test',revision:1,type:'collection',title:'常用',titleMode:'custom',layout:'list',bounds:{x:0,y:0,width:400,height:300,unit:'dip',displayId:''},expandedSize:{width:400,height:300},collapsed:false,positionLocked:false,items:[{id:'a',type:'path',path:'C:\\a.txt'},{id:'b',type:'path',path:'C:\\b.txt'}],rootPath:'',currentPath:'',expandedPaths:[],appearanceOverrides:{},menuOverrides:{}};
const snapshot={window:windowDef,windows:[windowDef],openWindowIds:['test'],global:{schemaVersion:1,theme:'dark',appearance:{},menus:{},hiddenFiles:false,reducedMotion:false,desktopMode:false,autoStart:false,shellIntegration:false,hotkey:''},appearance:{opacity:.95,scale:1,titleColor:'#eeeeee',backgroundColor:'#292929',folderColor:'#ecc161',textColor:'#eeeeee'},drives:[],warnings:[],tasks:[],status:{desktop:'',hotkey:'',autoStart:'',shellIntegration:''},pendingPins:[],systemDark:true};
let wrapper:ReturnType<typeof mount>;
function boot(handler?:(method:string,params:any)=>any){
 vi.stubGlobal('ResizeObserver',class{observe(){}disconnect(){}});
 vi.stubGlobal('fetch',async(_url:any,options:any)=>{const {method,params}=JSON.parse(options.body);const value=handler?.(method,params);return new Response(JSON.stringify(value===undefined?{result:method==='Snapshot'?snapshot:method==='StatPaths'?[{path:'C:\\a.txt',name:'a.txt',isDir:false,isLink:false,hidden:false,size:0,modified:''},{path:'C:\\b.txt',name:'b.txt',isDir:false,isLink:false,hidden:false,size:0,modified:''}]:[],error:''}:value));});
 wrapper=mount(App);return wrapper;
}
afterEach(()=>{wrapper?.unmount();vi.unstubAllGlobals();});
it('Delete on virtual selections asks to unpin, never recycles actual files',async()=>{
 const methods:string[]=[];boot(method=>{methods.push(method);});await flushPromises();
 await wrapper.find('[data-row-id="a"]').trigger('click');await wrapper.find('.content').trigger('keydown',{key:'Delete'});
 expect(wrapper.find('.dialog').text()).toContain('磁盘文件保持不变');await wrapper.find('.dialog form').exists();
 await wrapper.find('form.dialog').trigger('submit');await flushPromises();
 expect(methods).toContain('MutateEntries');expect(methods).not.toContain('Recycle');
});
it('editing text does not invoke file keyboard shortcuts',async()=>{
 const methods:string[]=[];boot(method=>{methods.push(method);});await flushPromises();
 await wrapper.find('[data-row-id="a"]').trigger('click');await wrapper.find('.content').trigger('keydown',{key:'F2'});
 await wrapper.find('.dialog input').trigger('keydown',{key:'Delete'});
 expect(wrapper.find('.dialog h2').text()).toBe('编辑显示名称');expect(methods).not.toContain('Recycle');
});
it('honors builtin menu enabled and display-name overrides',async()=>{
 boot(method=>method==='GetMenu'?{result:[{id:'open',name:'启动它',enabled:true},{id:'rename',name:'禁止改名',enabled:false}],error:''}:undefined);await flushPromises();
 await wrapper.find('[data-row-id="a"]').trigger('contextmenu',{clientX:10,clientY:10});await flushPromises();
 const buttons=wrapper.findAll('.context-menu button');expect(buttons.some(b=>b.text()==='启动它')).toBe(true);expect(buttons.some(b=>b.text().includes('禁止改名'))).toBe(false);
});
it('positions a fitting context menu at the click using its rendered size',async()=>{
 vi.stubGlobal('innerWidth',360);vi.stubGlobal('innerHeight',420);
 const measure=vi.spyOn(HTMLElement.prototype,'getBoundingClientRect').mockReturnValue(new DOMRect(0,0,220,180));
 try{boot();await flushPromises();await wrapper.find('.content').trigger('contextmenu',{clientX:50,clientY:100});await flushPromises();const menu=wrapper.find('.context-menu').element as HTMLElement;expect(menu.style.left).toBe('50px');expect(menu.style.top).toBe('100px');}finally{measure.mockRestore();}
});
it('keeps a context menu reachable near the bottom-right corner',async()=>{
 vi.stubGlobal('innerWidth',360);vi.stubGlobal('innerHeight',420);
 const measure=vi.spyOn(HTMLElement.prototype,'getBoundingClientRect').mockReturnValue(new DOMRect(0,0,220,180));
 try{boot();await flushPromises();await wrapper.find('.content').trigger('contextmenu',{clientX:340,clientY:390});await flushPromises();const menu=wrapper.find('.context-menu').element as HTMLElement;expect(menu.style.left).toBe('120px');expect(menu.style.top).toBe('210px');}finally{measure.mockRestore();}
});
it('starts native window dragging only beyond five DIP and does not collapse after a drag',async()=>{
 const requests:{method:string,params:any}[]=[];boot((method,params)=>{requests.push({method,params});});await flushPromises();
 wrapper.find('.titlebar').element.dispatchEvent(new MouseEvent('pointerdown',{bubbles:true,button:0,clientX:10,clientY:10}));
 window.dispatchEvent(new MouseEvent('pointermove',{bubbles:true,clientX:14,clientY:10}));await flushPromises();expect(requests.some(r=>r.method==='NativeDragStart')).toBe(false);
 window.dispatchEvent(new MouseEvent('pointermove',{bubbles:true,clientX:16,clientY:10}));await flushPromises();window.dispatchEvent(new MouseEvent('pointerup',{bubbles:true}));await flushPromises();
 expect(requests.filter(r=>r.method==='NativeDragStart')).toEqual([{method:'NativeDragStart',params:{windowId:'test'}}]);expect(requests.some(r=>r.method==='UpdateWindow')).toBe(false);
});
it('native real-path drop into a virtual window pins without transferring files',async()=>{
 const requests:{method:string,params:any}[]=[];boot((method,params)=>{requests.push({method,params});});await flushPromises();
 Object.defineProperty(document,'elementFromPoint',{configurable:true,value:()=>null});
 window.dispatchEvent(new CustomEvent('deskgrid:drop',{detail:{windowId:'test',paths:['C:\\source.txt'],ctrl:false,shift:false,x:20,y:60}}));await flushPromises();
 expect(requests.filter(r=>r.method==='Pin')).toEqual([{method:'Pin',params:{windowId:'test',paths:['C:\\source.txt']}}]);expect(requests.some(r=>r.method==='TransferFiles')).toBe(false);
});
it('native capability errors remain visible to the user',async()=>{
 boot();await flushPromises();window.dispatchEvent(new CustomEvent('deskgrid:error',{detail:{message:'Native drag source unavailable'}}));await flushPromises();
 expect(wrapper.find('[role="alert"]').text()).toContain('Native drag source unavailable');
});
it('group header edits inline and Enter saves the virtual name without a rename dialog',async()=>{
 const requests:{method:string,params:any}[]=[];
 const grouped={...windowDef,layout:'grid',items:[{id:'g',type:'group',name:'工具',items:[{id:'a',type:'path',path:'C:\\a.txt'}]}]};
 boot((method,params)=>{requests.push({method,params});return method==='Snapshot'?{result:{...snapshot,window:grouped,windows:[grouped]},error:''}:undefined;});await flushPromises();
 await wrapper.find('[data-row-id="g"]').trigger('click');await wrapper.find('.group-title').trigger('click');
 expect(wrapper.find('.dialog').exists()).toBe(false);
 const input=wrapper.find('input[aria-label="应用文件夹名称"]');expect(input.exists()).toBe(true);
 await input.setValue('开发工具');await input.trigger('keydown',{key:'Enter'});await flushPromises();
 expect(requests.filter(r=>r.method==='MutateEntries')).toEqual([{method:'MutateEntries',params:{windowId:'test',operation:'rename',ids:['g'],name:'开发工具'}}]);
 expect(wrapper.find('input[aria-label="应用文件夹名称"]').exists()).toBe(false);expect(wrapper.find('.group-title').text()).toBe('开发工具');
});
it('Escape cancels an inline group name edit while leaving the group open',async()=>{
 const methods:string[]=[];const grouped={...windowDef,layout:'grid',items:[{id:'g',type:'group',name:'工具',items:[]}]};
 boot(method=>{methods.push(method);return method==='Snapshot'?{result:{...snapshot,window:grouped,windows:[grouped]},error:''}:undefined;});await flushPromises();
 await wrapper.find('[data-row-id="g"]').trigger('click');await wrapper.find('.group-title').trigger('click');
 const input=wrapper.find('input[aria-label="应用文件夹名称"]');expect(input.exists()).toBe(true);await input.setValue('不保存');await input.trigger('keydown',{key:'Escape'});await flushPromises();
 expect(wrapper.find('.group-panel').exists()).toBe(true);expect(wrapper.find('.group-title').text()).toBe('工具');expect(methods).not.toContain('MutateEntries');
});
it('native drop uses host directory attributes instead of element-relative final coordinates',async()=>{
 const requests:{method:string,params:any}[]=[];const folder={...windowDef,type:'folder',rootPath:'C:\\root',currentPath:'C:\\root',items:[]};
 boot((method,params)=>{requests.push({method,params});return method==='Snapshot'?{result:{...snapshot,window:folder,windows:[folder]},error:''}:method==='ListDirectory'?{result:{entries:[],total:0},error:''}:undefined;});await flushPromises();
 const wrong=document.createElement('div');wrong.dataset.directory='C:\\root\\wrong';Object.defineProperty(document,'elementFromPoint',{configurable:true,value:()=>wrong});
 window.dispatchEvent(new CustomEvent('deskgrid:drop',{detail:{windowId:'test',paths:['C:\\source.txt'],ctrl:true,shift:false,x:1,y:1,attributes:{'data-directory':'C:\\root\\child'}}}));await flushPromises();
 expect(requests.filter(r=>r.method==='TransferFiles')).toEqual([{method:'TransferFiles',params:{windowId:'test',paths:['C:\\source.txt'],destination:'C:\\root\\child',operation:'copy',conflict:'ask'}}]);
});
it('scroll activity fades after 800ms without changing the stored scroll position',async()=>{
 boot();await flushPromises();vi.useFakeTimers();
 try{const content=wrapper.find('.content');(content.element as HTMLElement).scrollTop=30;await content.trigger('scroll');expect(content.classes()).toContain('scroll-active');await vi.advanceTimersByTimeAsync(799);expect(content.classes()).toContain('scroll-active');await vi.advanceTimersByTimeAsync(1);expect(content.classes()).not.toContain('scroll-active');expect((content.element as HTMLElement).scrollTop).toBe(30);}finally{vi.useRealTimers();}
});
