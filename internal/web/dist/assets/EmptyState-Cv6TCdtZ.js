import{j as e,g as o}from"./index-DsYR9Q2j.js";import{c as n}from"./createLucideIcon-CBsg8tly.js";import{T as l}from"./triangle-alert-Be2-6b2d.js";/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const u=n("Inbox",[["polyline",{points:"22 12 16 12 14 15 10 15 8 12 2 12",key:"o97t9d"}],["path",{d:"M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z",key:"oot6mr"}]]);/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const h=n("Lock",[["rect",{width:"18",height:"11",x:"3",y:"11",rx:"2",ry:"2",key:"1w4ew1"}],["path",{d:"M7 11V7a5 5 0 0 1 10 0v4",key:"fwvmzm"}]]);/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const f=n("SearchX",[["path",{d:"m13.5 8.5-5 5",key:"1cs55j"}],["path",{d:"m8.5 8.5 5 5",key:"a8mexj"}],["circle",{cx:"11",cy:"11",r:"8",key:"4ej97u"}],["path",{d:"m21 21-4.3-4.3",key:"1qie3q"}]]),y={"no-data":{icon:u,tone:"text-muted-foreground"},"no-permission":{icon:h,tone:"text-muted-foreground"},"loading-fail":{icon:l,tone:"text-destructive"},"search-no-result":{icon:f,tone:"text-muted-foreground"}},g={sm:{container:"px-4 py-6 gap-2",iconWrap:"p-2",iconSize:"h-4 w-4",title:"text-sm",description:"text-xs"},md:{container:"px-6 py-10 gap-3",iconWrap:"p-3",iconSize:"h-6 w-6",title:"text-sm",description:"text-xs"},lg:{container:"px-8 py-16 gap-4",iconWrap:"p-4",iconSize:"h-8 w-8",title:"text-base",description:"text-sm"}};function v({icon:a,title:d,description:r,action:i,className:m,variant:p="no-data",size:x="md"}){const c=y[p],s=a??c.icon,t=g[x];return e.jsxs("div",{role:"status",className:o("flex flex-col items-center justify-center rounded-md border border-dashed border-border bg-muted/30 text-center",t.container,m),children:[s&&e.jsx("div",{className:o("rounded-full bg-muted",t.iconWrap,c.tone),children:e.jsx(s,{className:t.iconSize,"aria-hidden":!0})}),e.jsxs("div",{className:"space-y-1",children:[e.jsx("p",{className:o("font-medium text-foreground",t.title),children:d}),r&&e.jsx("p",{className:o("text-muted-foreground",t.description),children:r})]}),i&&e.jsx("div",{className:"pt-1",children:i})]})}export{v as E,u as I};
