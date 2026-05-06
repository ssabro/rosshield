import{r as o}from"./index-BD2Cmn5u.js";/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const f=t=>t.replace(/([a-z0-9])([A-Z])/g,"$1-$2").toLowerCase(),i=(...t)=>t.filter((e,r,n)=>!!e&&e.trim()!==""&&n.indexOf(e)===r).join(" ").trim();/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */var p={xmlns:"http://www.w3.org/2000/svg",width:24,height:24,viewBox:"0 0 24 24",fill:"none",stroke:"currentColor",strokeWidth:2,strokeLinecap:"round",strokeLinejoin:"round"};/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const w=o.forwardRef(({color:t="currentColor",size:e=24,strokeWidth:r=2,absoluteStrokeWidth:n,className:s="",children:a,iconNode:c,...u},l)=>o.createElement("svg",{ref:l,...p,width:e,height:e,stroke:t,strokeWidth:n?Number(r)*24/Number(e):r,className:i("lucide",s),...u},[...c.map(([m,d])=>o.createElement(m,d)),...Array.isArray(a)?a:[a]]));/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const g=(t,e)=>{const r=o.forwardRef(({className:n,...s},a)=>o.createElement(w,{ref:a,iconNode:e,className:i(`lucide-${f(t)}`,n),...s}));return r.displayName=`${t}`,r};var x=o.createContext(void 0);function h(t){const e=o.useContext(x);return t||e||"ltr"}function b(t,[e,r]){return Math.min(r,Math.max(e,t))}export{b as a,g as c,h as u};
