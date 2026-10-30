import math,sys
def h2r(h): h=h.lstrip('#'); return [int(h[i:i+2],16)/255 for i in (0,2,4)]
def r2h(c): return '#'+''.join('%02X'%round(max(0,min(1,x))*255) for x in c)
def lin(x): return x/12.92 if x<=0.04045 else ((x+0.055)/1.055)**2.4
def enc(x): return 12.92*x if x<=0.0031308 else 1.055*x**(1/2.4)-0.055
def lum(h): r,g,b=[lin(x) for x in h2r(h)]; return 0.2126*r+0.7152*g+0.0722*b
def cr(a,b): la,lb=sorted([lum(a),lum(b)],reverse=True); return (la+0.05)/(lb+0.05)
def to_oklch(h):
    r,g,b=[lin(x) for x in h2r(h)]
    l=0.4122214708*r+0.5363325363*g+0.0514459929*b; m=0.2119034982*r+0.6806995451*g+0.1073969566*b; s=0.0883024619*r+0.2817188376*g+0.6299787005*b
    l,m,s=[math.copysign(abs(v)**(1/3),v) for v in (l,m,s)]
    L=0.2104542553*l+0.7936177850*m-0.0040720468*s; A=1.9779984951*l-2.4285922050*m+0.4505937099*s; B=0.0259040371*l+0.7827717662*m-0.8086757660*s
    return L, math.hypot(A,B), math.degrees(math.atan2(B,A))%360
def from_oklch(L,C,H):
    A=C*math.cos(math.radians(H)); B=C*math.sin(math.radians(H))
    l=(L+0.3963377774*A+0.2158037573*B)**3; m=(L-0.1055613458*A-0.0638541728*B)**3; s=(L-0.0894841775*A-1.2914855480*B)**3
    r=4.0767416621*l-3.3077115913*m+0.2309699292*s; g=-1.2684380046*l+2.6097574011*m-0.3413193965*s; b=-0.0041960771*l-0.7034186147*m+1.7076147010*s
    return [enc(max(0,min(1,v))) for v in (r,g,b)]
def gamut(L,C,H):
    # reduce chroma until in sRGB
    while C>0:
        A=C*math.cos(math.radians(H)); B=C*math.sin(math.radians(H))
        l=(L+0.3963377774*A+0.2158037573*B)**3; m=(L-0.1055613458*A-0.0638541728*B)**3; s=(L-0.0894841775*A-1.2914855480*B)**3
        rgb=[4.0767416621*l-3.3077115913*m+0.2309699292*s,-1.2684380046*l+2.6097574011*m-0.3413193965*s,-0.0041960771*l-0.7034186147*m+1.7076147010*s]
        if all(-0.0005<=v<=1.0005 for v in rgb): break
        C-=0.002
    return r2h(from_oklch(L,max(C,0),H))
S,CV,SK,W='#FFFFFF','#F5F6F8','#EDEFF2','#FFFFFF'
DANGER_H=to_oklch('#B3261E')[2]; SUCCESS_H=to_oklch('#167033')[2]; WARN_H=to_oklch('#985000')[2]
def ok600(h): return cr(W,h)>=4.5 and min(cr(h,S),cr(h,CV),cr(h,SK))>=4.5
def derive(inp):
    L,C,H=to_oklch(inp); p=inp.upper(); adj=False
    while not ok600(p): L-=0.005; p=gamut(L,C,H); adj=True
    L6,C6,_=to_oklch(p)
    p7=gamut(L6-0.07,C6,H); p8=gamut(L6-0.14,C6*0.9,H)
    p50=gamut(0.97,min(C6*0.25,0.03),H); p100=gamut(0.93,min(C6*0.35,0.05),H)
    return dict(input=inp.upper(),adjusted=adj,p600=p,p700=p7,p800=p8,p50=p50,p100=p100,H=H,L=L6,C=C6)
for name,inp in [(a.split('=')[0],a.split('=')[1]) for a in sys.argv[1:]]:
    d=derive(inp)
    def hd(a,b): x=abs(a-b)%360; return min(x,360-x)
    print(f"## {name} input {d['input']} -> 600 {d['p600']} {'(ADJUSTED darker: input fails)' if d['adjusted'] else ''}  OKLCH L={d['L']:.3f} C={d['C']:.3f} H={d['H']:.0f}")
    print(f"   input checks: white on input {cr(W,inp):.2f}, input on sunken {cr(inp,SK):.2f}")
    print(f"   700 {d['p700']} 800 {d['p800']} 50 {d['p50']} 100 {d['p100']}")
    print(f"   white/600 {cr(W,d['p600']):.2f} white/700 {cr(W,d['p700']):.2f} 600/surface {cr(d['p600'],S):.2f} 600/canvas {cr(d['p600'],CV):.2f} 600/sunken {cr(d['p600'],SK):.2f} 800/50 {cr(d['p800'],d['p50']):.2f} 800/100 {cr(d['p800'],d['p100']):.2f} 700/50 {cr(d['p700'],d['p50']):.2f} focus600/canvas {cr(d['p600'],CV):.2f} 50/surface {cr(d['p50'],S):.2f}")
    print(f"   hue distance: danger {hd(d['H'],DANGER_H):.0f}°, success {hd(d['H'],SUCCESS_H):.0f}°, warning {hd(d['H'],WARN_H):.0f}°")
print("status hues", round(DANGER_H), round(SUCCESS_H), round(WARN_H))
for a,b,n in [('#141A23',CV,'text/canvas'),('#46505E',SK,'secondary/sunken'),('#5E6878',SK,'muted/sunken'),('#7C8696',S,'border/surface'),('#7A3F00','#FFF6E5','warn800/50'),('#0F5326','#EAF7EE','succ800/50'),('#8F1F18','#FDEEEC','dang800/50'),('#46505E','#EDEFF2','draft'),('#16348A','#EEF3FF','closed'),('#B86200',S,'warn600/surface'),('#D0342A',S,'danger600/surface'),('#1E8A42',S,'succ600/surface'),('#0C78BF',S,'info600/surface'),('#084C79','#EAF4FB','info800/50')]:
    print(f"{n} {cr(a,b):.2f}")
