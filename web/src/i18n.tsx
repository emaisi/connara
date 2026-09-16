import { createContext, useContext, useLayoutEffect, useMemo, useState, type ReactNode } from "react";
import english from "./locales/en.json";

export type Language = "zh-CN" | "en";

interface LanguageContextValue {
  language: Language;
  setLanguage: (language: Language) => void;
}

const LanguageContext = createContext<LanguageContextValue>({
  language: "zh-CN",
  setLanguage: () => undefined,
});

const translations = english as Record<string, string>;
const phrases = Object.entries(translations)
  .filter(([source]) => source.length >= 2)
  .sort(([left], [right]) => right.length - left.length);
interface TranslationState {
  source: string;
  rendered: string;
}
const originalText = new WeakMap<Text, TranslationState>();
const originalAttributes = new WeakMap<Element, Map<string, TranslationState>>();
const translatedAttributes = ["aria-label", "placeholder", "title"] as const;

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [language, setLanguage] = useState<Language>(() => {
    const saved = window.localStorage.getItem("apihub.language");
    return saved === "en" ? "en" : "zh-CN";
  });

  useLayoutEffect(() => {
    document.documentElement.lang = language;
    window.localStorage.setItem("apihub.language", language);
    translateTree(document.body, language);
    const observer = new MutationObserver((mutations) => {
      for (const mutation of mutations) {
        if (mutation.type === "characterData") translateTextNode(mutation.target as Text, language);
        if (mutation.type === "attributes") translateElement(mutation.target as Element, language);
        for (const node of mutation.addedNodes) translateTree(node, language);
      }
    });
    observer.observe(document.body, {
      childList: true,
      subtree: true,
      characterData: true,
      attributes: true,
      attributeFilter: [...translatedAttributes],
    });
    return () => observer.disconnect();
  }, [language]);

  const value = useMemo(() => ({ language, setLanguage }), [language]);
  return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>;
}

export function useLanguage(): LanguageContextValue {
  return useContext(LanguageContext);
}

export function translate(value: string, language: Language): string {
  if (language === "zh-CN" || !hasChinese(value)) return value;
  const leading = value.match(/^\s*/)?.[0] ?? "";
  const trailing = value.match(/\s*$/)?.[0] ?? "";
  const content = value.slice(leading.length, value.length - trailing.length);
  if (translations[content]) return leading + translations[content] + trailing;
  let result = content;
  for (const [source, target] of phrases) {
    if (result.includes(source)) result = result.replaceAll(source, target);
  }
  return leading + result + trailing;
}

function translateTree(root: Node, language: Language) {
  if (root.nodeType === Node.TEXT_NODE) {
    translateTextNode(root as Text, language);
    return;
  }
  if (!(root instanceof Element) || shouldSkip(root)) return;
  translateElement(root, language);
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      return node instanceof Element && shouldSkip(node) ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT;
    },
  });
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    if (node.nodeType === Node.TEXT_NODE) translateTextNode(node as Text, language);
    else translateElement(node as Element, language);
  }
}

function translateTextNode(node: Text, language: Language) {
  if (node.parentElement && shouldSkip(node.parentElement)) return;
  const current = node.nodeValue ?? "";
  let state = originalText.get(node);
  if (!state) {
    if (!hasChinese(current)) return;
    state = { source: current, rendered: current };
    originalText.set(node, state);
  }
  // A different value came from React; keep it as the new source in either language.
  if (current !== state.rendered) state.source = current;
  const next = translate(state.source, language);
  state.rendered = next;
  if (current !== next) node.nodeValue = next;
}

function translateElement(element: Element, language: Language) {
  if (shouldSkip(element)) return;
  let originals = originalAttributes.get(element);
  for (const name of translatedAttributes) {
    const current = element.getAttribute(name);
    if (current === null) {
      originals?.delete(name);
      continue;
    }
    let state = originals?.get(name);
    if (!state) {
      if (!hasChinese(current)) continue;
      originals ??= new Map<string, TranslationState>();
      state = { source: current, rendered: current };
      originals.set(name, state);
      originalAttributes.set(element, originals);
    }
    if (current !== state.rendered) state.source = current;
    const next = translate(state.source, language);
    state.rendered = next;
    if (current !== next) element.setAttribute(name, next);
  }
}

function shouldSkip(element: Element): boolean {
  return (
    ["CODE", "SCRIPT", "STYLE", "TEXTAREA"].includes(element.tagName) || element.closest('[translate="no"]') !== null
  );
}

function hasChinese(value: string): boolean {
  return /[\u3400-\u9fff]/.test(value);
}
