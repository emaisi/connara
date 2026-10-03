import { useEffect, useRef } from "react";
import { Annotation, EditorState, StateEffect, StateField } from "@codemirror/state";
import { Decoration, EditorView, keymap, lineNumbers, type DecorationSet } from "@codemirror/view";
import { javascript } from "@codemirror/lang-javascript";
import { bracketMatching } from "@codemirror/language";
import { autocompletion, type CompletionContext } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { search, searchKeymap, highlightSelectionMatches } from "@codemirror/search";
const externalValue = Annotation.define<boolean>();
const setHighlight = StateEffect.define<DecorationSet>();
const highlight = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update: (value, transaction) => {
    if (transaction.docChanged) return Decoration.none;
    for (const effect of transaction.effects) if (effect.is(setHighlight)) return effect.value;
    return value;
  },
  provide: (field) => EditorView.decorations.from(field),
});
export default function CodeEditor({
  value,
  onChange,
  readOnly = false,
  expanded = false,
  inputNames = [],
  insert,
  location,
}: {
  value: string;
  onChange: (value: string) => void;
  readOnly?: boolean;
  expanded?: boolean;
  inputNames?: string[];
  insert?: { text: string; sequence: number };
  location?: { line: number; column?: number; sequence: number };
}) {
  const host = useRef<HTMLDivElement>(null),
    view = useRef<EditorView | null>(null),
    callback = useRef(onChange);
  const names = useRef(inputNames);
  callback.current = onChange;
  names.current = inputNames;
  useEffect(() => {
    if (!host.current) return;
    const complete = (context: CompletionContext) => {
      const match = context.matchBefore(/input(?:\.[\w$]*)?$/);
      if (!match || (match.from === match.to && !context.explicit)) return null;
      return {
        from: match.from,
        options: names.current.map((name) => ({
          label: /^[A-Za-z_$][\w$]*$/.test(name) ? `input.${name}` : `input[${JSON.stringify(name)}]`,
          type: "property",
        })),
      };
    };
    view.current = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(),
          javascript(),
          bracketMatching(),
          history(),
          search(),
          highlightSelectionMatches(),
          autocompletion({ override: [complete] }),
          highlight,
          keymap.of([
            {
              key: "Escape",
              run: (editor) => {
                editor.setTabFocusMode(1000);
                return false;
              },
            },
            indentWithTab,
            ...defaultKeymap,
            ...historyKeymap,
            ...searchKeymap,
          ]),
          EditorView.lineWrapping,
          EditorState.readOnly.of(readOnly),
          EditorView.contentAttributes.of({ "aria-label": "JavaScript 代码" }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged && !update.transactions.every((transaction) => transaction.annotation(externalValue)))
              callback.current(update.state.doc.toString());
          }),
          EditorView.theme({
            "&": {
              minHeight: expanded ? "320px" : "120px",
              maxHeight: expanded ? "60vh" : "360px",
              border: "1px solid var(--border)",
              background: "var(--surface)",
              color: "var(--text)",
            },
            ".cm-scroller": { overflow: "auto" },
            ".cm-content": { fontFamily: "monospace" },
            ".workflow-code-error": { background: "rgba(220,38,38,.15)" },
          }),
        ],
      }),
    });
    return () => {
      view.current?.destroy();
      view.current = null;
    };
  }, [readOnly, expanded]);
  useEffect(() => {
    const editor = view.current;
    if (editor && editor.state.doc.toString() !== value)
      editor.dispatch({
        changes: { from: 0, to: editor.state.doc.length, insert: value },
        annotations: externalValue.of(true),
      });
  }, [value]);
  useEffect(() => {
    const editor = view.current;
    if (editor && insert) {
      editor.dispatch(editor.state.replaceSelection(insert.text));
      editor.focus();
    }
  }, [insert]);
  useEffect(() => {
    const editor = view.current;
    if (!editor || !location) return;
    const line = editor.state.doc.line(Math.max(1, Math.min(location.line, editor.state.doc.lines))),
      at = Math.min(line.to, line.from + Math.max(0, (location.column ?? 1) - 1));
    editor.dispatch({
      selection: { anchor: at },
      effects: [
        setHighlight.of(Decoration.set([Decoration.line({ class: "workflow-code-error" }).range(line.from)])),
        EditorView.scrollIntoView(at, { y: "center" }),
      ],
    });
    editor.focus();
  }, [location]);
  return <div className="min-w-0 overflow-hidden" ref={host} />;
}
