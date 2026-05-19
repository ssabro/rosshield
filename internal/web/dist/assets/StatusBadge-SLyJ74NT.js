import{j as e,g as n}from"./index-iZ2New7V.js";import{C as s}from"./circle-n5c5MAlw.js";import{c as t}from"./createLucideIcon-BZpN5_RA.js";import{C as o}from"./circle-check-B9Gc-ddG.js";/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const m=t("CircleDashed",[["path",{d:"M10.1 2.182a10 10 0 0 1 3.8 0",key:"5ilxe3"}],["path",{d:"M13.9 21.818a10 10 0 0 1-3.8 0",key:"11zvb9"}],["path",{d:"M17.609 3.721a10 10 0 0 1 2.69 2.7",key:"1iw5b2"}],["path",{d:"M2.182 13.9a10 10 0 0 1 0-3.8",key:"c0bmvh"}],["path",{d:"M20.279 17.609a10 10 0 0 1-2.7 2.69",key:"1ruxm7"}],["path",{d:"M21.818 10.1a10 10 0 0 1 0 3.8",key:"qkgqxc"}],["path",{d:"M3.721 6.391a10 10 0 0 1 2.7-2.69",key:"1mcia2"}],["path",{d:"M6.391 20.279a10 10 0 0 1-2.69-2.7",key:"1fvljs"}]]);/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const b=t("CircleX",[["circle",{cx:"12",cy:"12",r:"10",key:"1mglay"}],["path",{d:"m15 9-6 6",key:"1uzhvr"}],["path",{d:"m9 9 6 6",key:"z0biqf"}]]);/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const u=t("Clock",[["circle",{cx:"12",cy:"12",r:"10",key:"1mglay"}],["polyline",{points:"12 6 12 12 16 14",key:"68esgv"}]]);/**
 * @license lucide-react v0.469.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const k=t("Pause",[["rect",{x:"14",y:"4",width:"4",height:"16",rx:"1",key:"zuxfzm"}],["rect",{x:"6",y:"4",width:"4",height:"16",rx:"1",key:"1okwgv"}]]),x={running:{icon:s,className:"bg-sky-100 text-sky-900 dark:bg-sky-950 dark:text-sky-200 border-sky-300/40",defaultLabel:"진행 중",animated:!0},pending:{icon:u,className:"bg-slate-100 text-slate-700 dark:bg-slate-900 dark:text-slate-300 border-slate-300/40",defaultLabel:"대기"},queued:{icon:m,className:"bg-slate-100 text-slate-700 dark:bg-slate-900 dark:text-slate-300 border-slate-300/40",defaultLabel:"큐 대기"},success:{icon:o,className:"bg-emerald-100 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-200 border-emerald-300/40",defaultLabel:"성공"},failed:{icon:b,className:"bg-red-100 text-red-900 dark:bg-red-950 dark:text-red-200 border-red-300/40",defaultLabel:"실패"},paused:{icon:k,className:"bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200 border-amber-300/40",defaultLabel:"일시 정지"},unknown:{icon:s,className:"bg-muted text-muted-foreground border-border",defaultLabel:"알 수 없음"}};function f({status:r,label:d,showIcon:l=!0,className:c}){const a=x[r],i=a.icon;return e.jsxs("span",{className:n("inline-flex items-center gap-1.5 rounded border px-2 py-0.5 text-xs font-medium",a.className,c),"data-status":r,children:[l&&(a.animated?e.jsxs("span",{className:"relative inline-flex h-2 w-2","aria-hidden":!0,children:[e.jsx("span",{className:"absolute inset-0 rounded-full bg-current opacity-75 motion-safe:animate-ping"}),e.jsx("span",{className:"relative inline-flex h-2 w-2 rounded-full bg-current"})]}):e.jsx(i,{className:"size-3","aria-hidden":!0})),d??a.defaultLabel]})}export{f as S};
