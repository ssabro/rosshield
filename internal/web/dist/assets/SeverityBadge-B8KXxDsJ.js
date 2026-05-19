import{a as l,j as r,g as y}from"./index-BfIelVZx.js";import{c as t}from"./createLucideIcon-B0sodIQh.js";import{T as d}from"./triangle-alert-DitmjDRj.js";/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const m=t("CircleAlert",[["circle",{cx:"12",cy:"12",r:"10",key:"1mglay"}],["line",{x1:"12",x2:"12",y1:"8",y2:"12",key:"1pkeuh"}],["line",{x1:"12",x2:"12.01",y1:"16",y2:"16",key:"4dfq90"}]]);/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const p=t("Info",[["circle",{cx:"12",cy:"12",r:"10",key:"1mglay"}],["path",{d:"M12 16v-4",key:"1dtifu"}],["path",{d:"M12 8h.01",key:"e9boi3"}]]);/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const x=t("ShieldAlert",[["path",{d:"M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z",key:"oel41y"}],["path",{d:"M12 8v4",key:"1got3b"}],["path",{d:"M12 16h.01",key:"1drbdi"}]]);/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const h=t("TrendingDown",[["polyline",{points:"22 17 13.5 8.5 8.5 13.5 2 7",key:"1r2t7k"}],["polyline",{points:"16 17 22 17 22 11",key:"11uiuu"}]]),g={critical:"bg-severity-critical text-white dark:text-slate-950",high:"bg-severity-high text-white dark:text-slate-950",medium:"bg-severity-medium text-white dark:text-slate-950",low:"bg-severity-low text-white dark:text-slate-950",info:"bg-severity-info text-white dark:text-slate-950"},u={critical:x,high:d,medium:m,low:h,info:p},k={critical:"severity.critical",high:"severity.high",medium:"severity.medium",low:"severity.low",info:"severity.info"},f={sm:{wrapper:"px-1.5 py-0.5 text-[10px]",icon:"size-2.5"},md:{wrapper:"px-2 py-0.5 text-xs",icon:"size-3"}};function S({severity:e,showIcon:s=!0,size:a="md",className:n}){const o=l(),c=u[e],i=f[a];return r.jsxs("span",{className:y("inline-flex items-center gap-1 rounded font-medium",i.wrapper,g[e],n),"data-severity":e,children:[s&&r.jsx(c,{className:i.icon,"aria-hidden":!0}),o(k[e])]})}export{S};
