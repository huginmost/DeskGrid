import type {FileEntry} from './types';
export function selectItems(ids:string[],id:string,selected:Set<string>,anchor:string,keys:{shift:boolean,ctrl:boolean}):Set<string>{
 if(keys.shift && ids.includes(anchor)){
  const start=ids.indexOf(anchor),end=ids.indexOf(id);
  return new Set([...(keys.ctrl?selected:[]),...ids.slice(Math.min(start,end),Math.max(start,end)+1)]);
 }
 if(keys.ctrl){const next=new Set(selected);next.has(id)?next.delete(id):next.add(id);return next;}
 return new Set([id]);
}
export function dropIntent(target:'start'|'collection'|'folder',_source:'real'|'virtual',ctrl:boolean,shift:boolean):'pin'|'copy'|'move'{
 if(target!=='folder')return 'pin';
 return ctrl&&!shift?'copy':'move';
}
export function relativePath(root:string,path:string):string|null{
 const clean=(p:string)=>p.replaceAll('/','\\').replace(/\\+$/,'');
 const r=clean(root),p=clean(path);
 if(p.toLowerCase()===r.toLowerCase())return '.';
 return p.toLowerCase().startsWith(r.toLowerCase()+'\\')?p.slice(r.length+1):null;
}
export function createRPC(fetcher:typeof fetch=(...args)=>fetch(...args)){return async<T=unknown>(method:string,params:Record<string,unknown>={}):Promise<T>=>{
 const response=await fetcher('/api/rpc',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({method,params})});
 if(!response.ok)throw new Error(`服务请求失败 (${response.status})`);
 const data=await response.json();
 if(data.error)throw new Error(data.error);
 return data.result as T;
};}
export const rpc=createRPC();
export type DirectoryPage={entries:FileEntry[],total:number,loading?:boolean,error?:string};
export class DirectoryStore {
 cache=new Map<string,DirectoryPage>();
 private versions=new Map<string,number>();
 private loader:(path:string,offset:number,limit:number)=>Promise<DirectoryPage>;
 constructor(loader:(path:string,offset:number,limit:number)=>Promise<DirectoryPage>){this.loader=loader;}
 async load(path:string,append=false):Promise<void>{
  const version=(this.versions.get(path)||0)+1;this.versions.set(path,version);
  const before=this.cache.get(path),offset=append?(before?.entries.length||0):0;
  this.cache.set(path,{entries:before?.entries||[],total:before?.total||0,loading:true});
  try{
   const result=await this.loader(path,offset,append?500:Math.max(500,before?.entries.length||0));
   if(this.versions.get(path)!==version)return;
   this.cache.set(path,{entries:append?[...(before?.entries||[]),...result.entries]:result.entries,total:result.total});
  }catch(e){if(this.versions.get(path)===version)this.cache.set(path,{entries:before?.entries||[],total:before?.total||0,error:String((e as Error).message||e)});}
 }
}
