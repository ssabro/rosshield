// E10 Stage D — Vitest setup.
//
// @testing-library/jest-dom의 expect matcher 등록 + DOM 정리 후크.
// D-UI-1 Stage 5 — vitest-axe matcher (`toHaveNoViolations`) 등록.
import "@testing-library/jest-dom/vitest";
import { expect, afterEach } from "vitest";
import { cleanup } from "@testing-library/react";
import * as axeMatchers from "vitest-axe/matchers";
import type { AxeMatchers } from "vitest-axe/matchers";

expect.extend(axeMatchers);

// jest-dom 의 `Assertion<T = any>` 와 type parameter 시그니처를 맞춰야
// "All declarations of 'Assertion' must have identical type parameters" (TS2428) 회피.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
declare module "vitest" {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  interface Assertion<T = any> extends AxeMatchers {
    // marker to keep T in scope; AxeMatchers covers the actual API.
    _axeT?: T;
  }
  interface AsymmetricMatchersContaining extends AxeMatchers {}
}

afterEach(() => {
  cleanup();
});
