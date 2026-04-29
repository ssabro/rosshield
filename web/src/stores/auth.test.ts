// E10 Stage D — auth 스토어 단위 테스트.
import { beforeEach, describe, expect, it } from "vitest";
import { useAuthStore } from "./auth";

describe("useAuthStore", () => {
  beforeEach(() => {
    useAuthStore.getState().clearSession();
    localStorage.clear();
  });

  it("starts empty", () => {
    const s = useAuthStore.getState();
    expect(s.accessToken).toBeNull();
    expect(s.refreshToken).toBeNull();
    expect(s.user).toBeNull();
  });

  it("setSession populates token + user", () => {
    useAuthStore.getState().setSession({
      accessToken: "at_x",
      refreshToken: "rt_y",
      user: { id: "us_1", email: "a@b.c", displayName: "A", tenantId: "tn_1" },
    });
    const s = useAuthStore.getState();
    expect(s.accessToken).toBe("at_x");
    expect(s.refreshToken).toBe("rt_y");
    expect(s.user?.email).toBe("a@b.c");
  });

  it("clearSession resets to nulls", () => {
    useAuthStore.getState().setSession({
      accessToken: "at",
      refreshToken: "rt",
      user: { id: "1", email: "x", displayName: "X", tenantId: "t" },
    });
    useAuthStore.getState().clearSession();
    const s = useAuthStore.getState();
    expect(s.accessToken).toBeNull();
    expect(s.user).toBeNull();
  });

  it("persists to localStorage under 'rosshield-auth'", () => {
    useAuthStore.getState().setSession({
      accessToken: "persist-me",
      refreshToken: "rt",
      user: { id: "1", email: "x", displayName: "X", tenantId: "t" },
    });
    const raw = localStorage.getItem("rosshield-auth");
    expect(raw).toBeTruthy();
    expect(raw).toContain("persist-me");
  });
});
