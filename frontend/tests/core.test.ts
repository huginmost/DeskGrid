import {test} from 'node:test';
import assert from 'node:assert/strict';
import {selectItems,dropIntent,relativePath,DirectoryStore,createRPC} from '../src/behavior.ts';
test('selection retains loaded offscreen range and Ctrl toggles',()=>{
 assert.deepEqual(selectItems(['a','b','c','d'],'d',new Set(['b']),'b',{shift:true,ctrl:false}),new Set(['b','c','d']));
 assert.deepEqual(selectItems(['a','b'],'b',new Set(['a','b']),'a',{shift:false,ctrl:true}),new Set(['a']));
});
test('virtual drop cannot silently move target files',()=>{
 assert.equal(dropIntent('folder','virtual',false,false),'copy');
 assert.equal(dropIntent('folder','virtual',false,true),'confirmMove');
 assert.equal(dropIntent('collection','real',false,false),'pin');
 assert.equal(dropIntent('folder','real',true,false),'copy');
 assert.equal(dropIntent('folder','real',false,false),'move');
});
test('relative path cannot escape similarly named root',()=>{
 assert.equal(relativePath('C:\\work','C:\\work\\中文\\a.txt'),'中文\\a.txt');
 assert.equal(relativePath('C:\\work','C:\\work'),'.');
 assert.equal(relativePath('C:\\work','C:\\workspace\\a'),null);
});
test('latest directory refresh wins over stale responses',async()=>{
 let resolveOld!:(v:any)=>void; let calls=0;
 const store=new DirectoryStore(async()=>++calls===1?new Promise(r=>resolveOld=r):{entries:[{path:'new',name:'new'}],total:1});
 const old=store.load('C:\\'); await store.load('C:\\');
 resolveOld({entries:[{path:'old',name:'old'}],total:1}); await old;
 assert.deepEqual(store.cache.get('C:\\')?.entries.map(e=>e.path),['new']);
});
test('HTTP success does not hide backend failure',async()=>{
 const rpc=createRPC(async()=>new Response(JSON.stringify({result:null,error:'Access denied'}),{status:200})) as any;
 await assert.rejects(rpc('Recycle',{paths:['C:\\a']}),/Access denied/);
});
