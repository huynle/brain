// CodeMirror language + extension setup shared by the entry editor and the
// compose modal. Brain entries are YAML frontmatter + a markdown body, so we
// use lang-yaml's yamlFrontmatter wrapper around the markdown language to
// highlight both regions correctly.

import { markdown, markdownLanguage } from "@codemirror/lang-markdown";
import { yamlFrontmatter } from "@codemirror/lang-yaml";
import { vim } from "@replit/codemirror-vim";
import type { Extension } from "@codemirror/state";

/** Markdown body with a highlighted leading `---` YAML frontmatter block. */
export function brainLanguage(): Extension {
  return yamlFrontmatter({
    content: markdown({ base: markdownLanguage }),
  });
}

const VIM_PREF_KEY = "brain.editor_vim";

export function vimEnabled(): boolean {
  return localStorage.getItem(VIM_PREF_KEY) === "1";
}

export function setVimEnabled(on: boolean): void {
  localStorage.setItem(VIM_PREF_KEY, on ? "1" : "0");
}

/** Editor extensions, ordered so vim (when on) takes keymap precedence. */
export function editorExtensions(useVim: boolean): Extension[] {
  const exts: Extension[] = [];
  if (useVim) exts.push(vim());
  exts.push(brainLanguage());
  return exts;
}
