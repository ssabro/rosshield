// E10 Stage D — ApiError + extractErrorMessage 단위 테스트.
import { describe, expect, it } from "vitest";
import { ApiError, extractErrorMessage } from "./errors";

describe("ApiError", () => {
  it("classifies 4xx as client error", () => {
    const e = new ApiError(404, "not found");
    expect(e.isClientError()).toBe(true);
    expect(e.isServerError()).toBe(false);
    expect(e.isUnauthorized()).toBe(false);
  });

  it("classifies 401 as unauthorized", () => {
    const e = new ApiError(401, "no token");
    expect(e.isUnauthorized()).toBe(true);
    expect(e.isClientError()).toBe(true);
  });

  it("classifies 5xx as server error", () => {
    const e = new ApiError(503, "down");
    expect(e.isServerError()).toBe(true);
    expect(e.isClientError()).toBe(false);
  });

  it("preserves status and message via Error chain", () => {
    const e = new ApiError(400, "bad request");
    expect(e.status).toBe(400);
    expect(e.message).toBe("bad request");
    expect(e.name).toBe("ApiError");
    expect(e instanceof Error).toBe(true);
  });
});

describe("extractErrorMessage", () => {
  it("returns server-provided error message", () => {
    expect(extractErrorMessage({ error: "invalid creds" }, "fallback")).toBe(
      "invalid creds"
    );
  });

  it("falls back when error field absent", () => {
    expect(extractErrorMessage({ wat: 1 }, "fallback")).toBe("fallback");
  });

  it("falls back on null/undefined", () => {
    expect(extractErrorMessage(null, "fb")).toBe("fb");
    expect(extractErrorMessage(undefined, "fb")).toBe("fb");
  });

  it("falls back when error message empty", () => {
    expect(extractErrorMessage({ error: "" }, "fb")).toBe("fb");
  });
});
