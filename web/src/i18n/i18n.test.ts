import { afterEach, describe, expect, it, vi } from "vitest";

import en from "./locales/en.json";
import zhTW from "./locales/zh-TW.json";

import i18next, {
  changeLanguage,
  currentLanguage,
  defaultLanguage,
  readStoredLanguage,
  resolveLanguage,
  storageKey,
  storeLanguage,
} from "./index";

afterEach(() => {
  vi.unstubAllGlobals();
  window.localStorage.clear();
});

describe("locale resources", () => {
  it("cover the same keys in both languages", () => {
    expect(Object.keys(en).sort()).toEqual(Object.keys(zhTW).sort());
  });

  it("have no empty values", () => {
    for (const resource of [en, zhTW]) {
      for (const [key, value] of Object.entries(resource)) {
        expect(value, key).not.toBe("");
      }
    }
  });
});

describe("resolveLanguage", () => {
  it("prefers a supported stored language", () => {
    expect(resolveLanguage("en", ["zh-TW"])).toBe("en");
    expect(resolveLanguage("zh-TW", ["en-US"])).toBe("zh-TW");
  });

  it("ignores an unsupported stored language", () => {
    expect(resolveLanguage("de", ["en-US"])).toBe("en");
    expect(resolveLanguage("zh", ["en-US"])).toBe("en");
  });

  it("maps any zh browser language to zh-TW", () => {
    expect(resolveLanguage(null, ["zh-CN", "en"])).toBe("zh-TW");
    expect(resolveLanguage(null, ["ZH-Hant-TW"])).toBe("zh-TW");
  });

  it("falls back to en for other browser languages", () => {
    expect(resolveLanguage(null, ["ja-JP"])).toBe("en");
  });

  it("falls back to the default without any browser language", () => {
    expect(resolveLanguage(null, [])).toBe(defaultLanguage);
    expect(defaultLanguage).toBe("zh-TW");
  });
});

describe("language storage", () => {
  it("persists and reads back the language", () => {
    storeLanguage("en");

    expect(window.localStorage.getItem(storageKey)).toBe("en");
    expect(readStoredLanguage()).toBe("en");
  });

  it("survives a localStorage that throws", () => {
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
    });

    expect(() => storeLanguage("en")).not.toThrow();
    expect(readStoredLanguage()).toBeNull();
  });
});

describe("changeLanguage", () => {
  it("switches i18next and persists the choice", async () => {
    await changeLanguage("en");

    expect(currentLanguage()).toBe("en");
    expect(i18next.t("action.start")).toBe("Start");
    expect(readStoredLanguage()).toBe("en");

    await changeLanguage("zh-TW");

    expect(currentLanguage()).toBe("zh-TW");
    expect(i18next.t("action.start")).toBe("開始");
  });
});
