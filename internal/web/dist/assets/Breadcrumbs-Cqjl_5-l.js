import{j as r,L as c}from"./index-1_ZBBgry.js";import{c as l}from"./createLucideIcon-D8QrfWG7.js";/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const d=l("ChevronRight",[["path",{d:"m9 18 6-6-6-6",key:"mthhwq"}]]);function x({items:a}){return r.jsx("nav",{className:"flex items-center gap-1 text-xs text-muted-foreground","aria-label":"Breadcrumb",children:a.map((e,s)=>{const n=s===a.length-1,t=s>0&&r.jsx(d,{"aria-hidden":"true",className:"h-3 w-3 shrink-0"}),o=e.to&&!n?r.jsx(c,{to:e.to,params:e.params,className:"hover:text-foreground hover:underline",children:e.label}):r.jsx("span",{className:n?"font-medium text-foreground":"",children:e.label});return r.jsxs("span",{className:"flex items-center gap-1",children:[t,o]},s)})})}export{x as B};
