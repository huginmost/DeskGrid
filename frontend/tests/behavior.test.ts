import {describe,it,expect} from 'vitest';
import {selectItems,dropIntent,relativePath,DirectoryStore,createRPC} from '../src/behavior';

describe('selection',()=>{
 it('extends from the anchor over offscreen loaded entries',()=>{
  expect(selectItems(['a','b','c','d'],'d',new Set(['b']),'b',{shift:true,ctrl:false})).toEqual(new Set(['b','c','d']));
 });
 it('toggles a control-click without losing the other selections',()=>{
  expect(selectItems(['a','b'],'b',new Set(['a','b']),'a',{shift:false,ctrl:true})).toEqual(new Set(['a']));
 });
});
describe('drop intent safety',()=>{
 it('never moves target files when a virtual entry is dropped into a real folder without Shift',()=>{
  expect(dropIntent('folder','virtual',false,false)).toBe('copy');
  expect(dropIntent('folder','virtual',false,true)).toBe('confirmMove');
 });
 it('pins real paths into collections and respects Ctrl copying to real folders',()=>{
  expect(dropIntent('collection','real',false,false)).toBe('pin');
  expect(dropIntent('folder','real',true,false)).toBe('copy');
  expect(dropIntent('folder','real',false,false)).toBe('move');
 });
});
it('computes root-relative Windows paths without crossing another root',()=>{
 expect(relativePath('C:\\work','C:\\work\\中文\\a.txt')).toBe('中文\\a.txt');
 expect(relativePath('C:\\work','C:\\work')).toBe('.');
 expect(relativePath('C:\\work','C:\\workspace\\a')).toBeNull();
});
it('discards a delayed directory response superseded by a newer refresh',async()=>{
 let resolveOld!: (v:any)=>void;
 let calls=0;
 const store=new DirectoryStore(async()=>++calls===1?new Promise(r=>resolveOld=r):{entries:[{path:'new',name:'new'}],total:1});
 const old=store.load('C:\\');
 await store.load('C:\\');
 resolveOld({entries:[{path:'old',name:'old'}],total:1}); await old;
 expect(store.cache.get('C:\\')?.entries.map(e=>e.path)).toEqual(['new']);
});
it('does not treat an HTTP 200 RPC error as success',async()=>{
 const rpc=createRPC(async()=>new Response(JSON.stringify({result:null,error:'Access denied'}),{status:200}));
 await expect(rpc('Recycle',{paths:['C:\\a']})).rejects.toThrow('Access denied');
});
it('refresh preserves the loaded page extent instead of dropping offscreen selection rows',async()=>{
 let limit=0;
 const store=new DirectoryStore(async(_path,_offset,size)=>{limit=size;return {entries:Array.from({length:size},(_,i)=>({path:String(i),name:String(i),isDir:false,isLink:false,hidden:false,size:0,modified:''})),total:2000};});
 await store.load('C:\\');await store.load('C:\\',true);await store.load('C:\\');
 expect(limit).toBe(1000);expect(store.cache.get('C:\\')?.entries.length).toBe(1000);
});
