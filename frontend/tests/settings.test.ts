import {it,expect,vi} from 'vitest';
import {mount,flushPromises} from '@vue/test-utils';
import SettingsPanel from '../src/SettingsPanel.vue';
const windowDef={id:'w',title:'工作',type:'collection',appearanceOverrides:{titleColor:'#123456'},menuOverrides:{},items:[]};
const snapshot={window:windowDef,windows:[windowDef],appearance:{opacity:.95,scale:1,titleColor:'#123456',backgroundColor:'#282828',folderColor:'#f5c651',textColor:'#ffffff'},global:{theme:'dark',appearance:{},menus:{},hotkey:'Ctrl+Alt+S'},status:{desktop:'兼容模式',hotkey:'已注册',autoStart:'禁用',shellIntegration:'禁用'}} as any;
it('saving one appearance field preserves explicit fields without materializing inherited fields',async()=>{
 vi.useFakeTimers();let sent:any;
 vi.stubGlobal('fetch',async(_url:any,options:any)=>{sent=JSON.parse(options.body);return new Response(JSON.stringify({result:windowDef,error:''}));});
 const wrapper=mount(SettingsPanel,{props:{snapshot,windowId:'w'}});
 const opacity=wrapper.find('input[aria-label="透明度"]');expect(opacity.exists()).toBe(true);
 await opacity.setValue('60');await vi.advanceTimersByTimeAsync(350);await flushPromises();
 expect(sent).toEqual({method:'UpdateWindow',params:{windowId:'w',patch:{appearanceOverrides:{titleColor:'#123456',opacity:.6}}}});
 wrapper.unmount();vi.useRealTimers();vi.unstubAllGlobals();
});
it('global settings expose actual integration status and toggles',async()=>{
 const wrapper=mount(SettingsPanel,{props:{snapshot,windowId:'w'}});
 const buttons=wrapper.findAll('button');const global=buttons.find(b=>b.text()==='全局');expect(global).toBeDefined();await global!.trigger('click');
 expect(wrapper.text()).toContain('兼容模式');expect(wrapper.find('select[aria-label="主题"]').exists()).toBe(true);
});
it('changing the settings target loads its effective inherited appearance',async()=>{
 const second={...windowDef,id:'second',title:'另一个窗口',appearanceOverrides:{}};
 vi.stubGlobal('fetch',async(_url:any,options:any)=>{const request=JSON.parse(options.body);return new Response(JSON.stringify({result:request.method==='Snapshot'?{...snapshot,window:second,appearance:{...snapshot.appearance,backgroundColor:'#fafafa',opacity:.4}}:[],error:''}));});
 const wrapper=mount(SettingsPanel,{props:{snapshot:{...snapshot,windows:[windowDef,second]},windowId:'w'}});
 await wrapper.find('select[aria-label="正在设置的窗口"]').setValue('second');await flushPromises();
 expect((wrapper.find('input[aria-label="背景颜色"]').element as HTMLInputElement).value).toBe('#fafafa');
 expect((wrapper.find('input[aria-label="透明度"]').element as HTMLInputElement).value).toBe('40');
 wrapper.unmount();vi.unstubAllGlobals();
});
