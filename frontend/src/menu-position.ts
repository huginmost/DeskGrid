// Client coordinates, rendered menu size and viewport dimensions all use CSS pixels.
// Keep the pointer as the anchor whenever the menu fits; flip only overflowing axes.
export function contextMenuPosition(x:number,y:number,width:number,height:number,viewportWidth:number,viewportHeight:number):{x:number,y:number}{
 const margin=4;
 const position=(anchor:number,size:number,available:number)=>{
  const max=Math.max(margin,available-size-margin);
  const fits=anchor+size+margin<=available;
  const preferred=fits?anchor:anchor-size>=margin?anchor-size:max;
  return Math.max(margin,Math.min(preferred,max));
 };
 return {x:position(x,width,viewportWidth),y:position(y,height,viewportHeight)};
}
