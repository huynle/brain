/**
 * lib/messageModel — which model produced a transcript message.
 *
 * OpenCode records this two different ways depending on the role, and both
 * appear in real stored messages:
 *
 *   assistant  { agent, modelID: "claude-opus-4-5", providerID: "anthropic" }
 *   user       { agent, model: { modelID: "...", providerID: "..." } }
 *
 * Reading either field alone therefore shows the model on some rows and
 * nothing on others, which reads as "this turn had no model" rather than
 * "we looked in the wrong place". Everything goes through here.
 */
import type { OcMessageInfo } from "./types";

export interface MessageModel {
  /** Bare model id, e.g. "claude-opus-4-5". */
  id: string;
  /** Provider, e.g. "anthropic". "" when the payload omits it. */
  provider: string;
}

/** The model on one message, or null when the payload carries none. */
export function messageModel(info: OcMessageInfo): MessageModel | null {
  const flatID = typeof info.modelID === "string" ? info.modelID.trim() : "";
  const flatProvider =
    typeof info.providerID === "string" ? info.providerID.trim() : "";
  if (flatID) return { id: flatID, provider: flatProvider };

  const nested = info.model;
  if (typeof nested === "string") {
    // Not a shape OpenCode writes today, but a plain string is the obvious
    // way this field could arrive from another executor, and treating it as
    // an object would silently drop it.
    return shortenSlashed(nested.trim());
  }
  if (nested && typeof nested === "object") {
    const id = typeof nested.modelID === "string" ? nested.modelID.trim() : "";
    const provider =
      typeof nested.providerID === "string" ? nested.providerID.trim() : "";
    if (id) return { id, provider };
  }
  return null;
}

/** Splits a "provider/model" string into its two halves. */
function shortenSlashed(value: string): MessageModel | null {
  if (!value) return null;
  const slash = value.lastIndexOf("/");
  if (slash <= 0) return { id: value, provider: "" };
  return { id: value.slice(slash + 1), provider: value.slice(0, slash) };
}

/**
 * The label shown beside the agent name.
 *
 * Deliberately the bare model id, not "provider/model": the row is already
 * dense, the provider almost never varies within a session, and the point of
 * the chip is spotting the turn where the model CHANGED. The provider still
 * shows in the title tooltip, where the space is free.
 */
export function modelLabel(model: MessageModel): string {
  return model.id;
}

/** Full "provider/model" for a tooltip. */
export function modelTitle(model: MessageModel): string {
  return model.provider ? `${model.provider}/${model.id}` : model.id;
}
