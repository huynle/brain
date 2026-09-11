/** Segment PCM without releasing its capture stream. Keep a short pre-roll. */
export class VoiceSegmenter {
  private pre: Float32Array[] = [];
  private frames: Float32Array[] = [];
  private voiced = 0;
  private quiet = 0;
  private size = 0;
  private active = false;
  private rate: number;
  private started: () => void;
  private ended: (pcm: Float32Array) => void;
  constructor(rate: number, started: () => void, ended: (pcm: Float32Array) => void) {this.rate=rate;this.started=started;this.ended=ended;}
  reset() {this.pre=[];this.frames=[];this.voiced=0;this.quiet=0;this.size=0;this.active=false;}
  push(pcm: Float32Array) {
    let energy=0;for(const v of pcm) energy+=v*v;
    const voice=Math.sqrt(energy/pcm.length)>=.015;
    if(!this.active) {
      this.pre.push(pcm);
      while(this.pre.length>Math.ceil(this.rate*.3/pcm.length))this.pre.shift();
      this.voiced=voice?this.voiced+pcm.length:0;
      if(this.voiced<this.rate*.15)return;
      this.active=true;this.frames=this.pre;this.pre=[];
      this.size=this.frames.reduce((n,f)=>n+f.length,0);this.quiet=0;this.started();return;
    }
    this.frames.push(pcm);this.size+=pcm.length;
    this.quiet=voice?0:this.quiet+pcm.length;
    if(this.quiet>=this.rate*1.2 || this.size>=this.rate*30) {
      const result=new Float32Array(this.size);let offset=0;
      for(const f of this.frames){result.set(f,offset);offset+=f.length;}
      this.reset();this.ended(result);
    }
  }
}

export function pcmWav(pcm: Float32Array, rate: number): Uint8Array {
  const count=Math.floor(pcm.length*16000/rate), buffer=new ArrayBuffer(44+count*2), view=new DataView(buffer);
  const word=(s:string,o:number)=>{for(let i=0;i<s.length;i++)view.setUint8(o+i,s.charCodeAt(i));};
  word('RIFF',0);view.setUint32(4,36+count*2,true);word('WAVEfmt ',8);view.setUint32(16,16,true);
  view.setUint16(20,1,true);view.setUint16(22,1,true);view.setUint32(24,16000,true);view.setUint32(28,32000,true);view.setUint16(32,2,true);view.setUint16(34,16,true);word('data',36);view.setUint32(40,count*2,true);
  for(let i=0;i<count;i++){const start=Math.floor(i*rate/16000),end=Math.max(start+1,Math.floor((i+1)*rate/16000));let v=0;for(let j=start;j<end&&j<pcm.length;j++)v+=pcm[j];v/=end-start;view.setInt16(44+i*2,Math.round(Math.max(-1,Math.min(1,v))*32767),true);}
  return new Uint8Array(buffer);
}
