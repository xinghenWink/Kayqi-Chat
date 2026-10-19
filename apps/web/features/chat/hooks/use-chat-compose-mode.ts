"use client";

import * as React from "react";
import type { ChatModelOption } from "@/features/chat/types/chat-runtime";
import { type ChatComposeMode, modelForComposeMode, modelSupportsComposeMode } from "@/features/chat/model/image-parameter-policy";

type SavedMode = { scope: string; mode: ChatComposeMode; chatModel: string; imageModel: string };

// 会话模式按服务器、用户与会话隔离；存储不可用时仍可在内存中正常工作。
export function useChatComposeMode({ models, selectedModel, setSelectedModel, scope, initialImage, ready, temporary, requirePolicy = true, inheritNewConversation = false }: {
  models: ChatModelOption[]; selectedModel: string; setSelectedModel: (model: string) => void;
  scope: string; initialImage: boolean; ready: boolean; temporary: boolean; requirePolicy?: boolean; inheritNewConversation?: boolean;
}) {
  const [saved, setSaved] = React.useState<SavedMode>({ scope: "", mode: "chat", chatModel: "", imageModel: "" });
  const inherit = inheritNewConversation && saved.scope.includes(":new:");
  const mode: ChatComposeMode = !temporary && (saved.scope === scope || inherit) ? saved.mode : "chat";
  const visibleModels = React.useMemo(() => models.filter((model) => modelSupportsComposeMode(model, mode, requirePolicy)).map((model) => modelForComposeMode(model, mode)), [models, mode, requirePolicy]);

  React.useEffect(() => {
    if (!ready || models.length === 0 || saved.scope === scope) return;
    let restored: SavedMode = inherit ? { ...saved, scope } : { scope, mode: initialImage && !temporary ? "image" : "chat", chatModel: "", imageModel: "" };
    try {
      const raw: unknown = JSON.parse(window.sessionStorage.getItem(`kayqi:compose:v1:${scope}`) || "null");
      if (raw && typeof raw === "object" && "mode" in raw && (raw.mode === "chat" || raw.mode === "image")) {
        restored = { ...restored, mode: temporary ? "chat" : raw.mode,
          chatModel: "chatModel" in raw && typeof raw.chatModel === "string" ? raw.chatModel : "",
          imageModel: "imageModel" in raw && typeof raw.imageModel === "string" ? raw.imageModel : "" };
      }
    } catch { /* 隐私模式下只使用内存状态。 */ }
    setSaved(restored);
  }, [inherit, initialImage, models, ready, saved, scope, temporary]);

  React.useEffect(() => {
    if (saved.scope !== scope || !ready) return;
    const key = mode === "chat" ? "chatModel" : "imageModel";
    const selected = visibleModels.find((model) => model.platformModelName === selectedModel);
    if (!selected) {
      const fallback = visibleModels.find((model) => model.platformModelName === saved[key]) ?? visibleModels[0];
      setSelectedModel(fallback?.platformModelName ?? "");
      return;
    }
    if (saved[key] !== selectedModel) setSaved((current) => ({ ...current, [key]: selectedModel }));
  }, [mode, ready, saved, scope, selectedModel, setSelectedModel, visibleModels]);

  React.useEffect(() => {
    if (saved.scope !== scope || temporary) return;
    try { window.sessionStorage.setItem(`kayqi:compose:v1:${scope}`, JSON.stringify(saved)); } catch { /* 不影响提交。 */ }
  }, [saved, scope, temporary]);

  const changeMode = React.useCallback((next: ChatComposeMode, preferredModel?: string) => {
    if (temporary && next === "image") return false;
    const candidates = models.filter((model) => modelSupportsComposeMode(model, next, requirePolicy));
    const key = next === "chat" ? "chatModel" : "imageModel";
    const target = candidates.find((model) => model.platformModelName === preferredModel)
      ?? candidates.find((model) => model.platformModelName === saved[key]) ?? candidates[0];
    if (!target) return false;
    setSaved((current) => ({ ...current, scope, mode: next, [key]: target.platformModelName }));
    setSelectedModel(target.platformModelName);
    return true;
  }, [models, requirePolicy, saved, scope, setSelectedModel, temporary]);

  return { composeMode: mode, changeComposeMode: changeMode, modelOptions: visibleModels,
    imageAvailable: !temporary && models.some((model) => modelSupportsComposeMode(model, "image", requirePolicy)) };
}
