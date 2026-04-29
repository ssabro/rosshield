// E10 Stage D — Vitest setup.
//
// @testing-library/jest-dom의 expect matcher 등록 + DOM 정리 후크.
import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

afterEach(() => {
  cleanup();
});
