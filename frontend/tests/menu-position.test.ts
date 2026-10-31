import {it,expect} from 'vitest';
import {contextMenuPosition} from '../src/menu-position';

it('keeps a fitting menu anchored at the actual right-click location',()=>{
 expect(contextMenuPosition(50,100,220,180,360,420)).toEqual({x:50,y:100});
});
it('flips around the pointer only on axes that would overflow',()=>{
 expect(contextMenuPosition(340,390,220,180,360,420)).toEqual({x:120,y:210});
 expect(contextMenuPosition(50,390,220,180,360,420)).toEqual({x:50,y:210});
 expect(contextMenuPosition(340,100,220,180,360,420)).toEqual({x:120,y:100});
});
it('uses the actual menu height rather than a fixed large estimate',()=>{
 expect(contextMenuPosition(100,200,220,100,360,420)).toEqual({x:100,y:200});
 expect(contextMenuPosition(100,200,220,250,360,420)).toEqual({x:100,y:166});
});
it('keeps oversized menus inside the viewport with a small edge margin',()=>{
 expect(contextMenuPosition(200,100,600,800,220,120)).toEqual({x:4,y:4});
 expect(contextMenuPosition(0,0,180,80,220,120)).toEqual({x:4,y:4});
});
