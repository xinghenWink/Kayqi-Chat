"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { toPendingAttachments } from "@/features/chat/model/message-submit";
import { modelSupportsComposeMode } from "@/features/chat/model/image-parameter-policy";
import type { ChatAreaMessage } from "@/features/chat/types/messages";
import type { ChatModelOption, PendingAttachment } from "@/features/chat/types/chat-runtime";
import type { ConversationOptions } from "@/shared/api/conversation-types";

// 复用已保存的有效参数，切换模式完成后再写入，避免初始化 effect 覆盖用户选择。
export function useChatImageReuse({ messages, models, scope, composeMode, selectedModel, changeMode, setOptions, setDraft, setAttachments, releaseAttachments, attachments, ready }: {
 messages: ChatAreaMessage[]; models: ChatModelOption[]; scope: string; composeMode: string; selectedModel: string;
 changeMode: (mode: "image", preferred?: string) => boolean;
 setOptions: React.Dispatch<React.SetStateAction<ConversationOptions>>; setDraft: (value: string) => void;
 setAttachments: React.Dispatch<React.SetStateAction<PendingAttachment[]>>;
 releaseAttachments: (items: PendingAttachment[]) => void; attachments: PendingAttachment[]; ready: boolean;
}) {
 const t = useTranslations("chat.points");
 const [pending, setPending] = React.useState<{ scope: string; model: string; options: ConversationOptions } | null>(null);
 const [confirmationScope, setConfirmationScope] = React.useState<string | null>(null);
 const reuse = React.useCallback((message: ChatAreaMessage, regenerate = false) => {
  const saved = message.points;
  const model = models.find((item) => item.platformModelName === saved?.model && modelSupportsComposeMode(item, "image"));
  const source = messages.find((item) => item.publicID === message.parentPublicID && item.role === "user");
  if (!saved?.options || !model || !source || !changeMode("image", model.platformModelName)) { toast.error(t("reuseUnavailable")); return; }
  releaseAttachments(attachments);
  setAttachments(toPendingAttachments(source));
  setDraft(source.content);
  setPending({ scope, model: model.platformModelName, options: saved.options });
  setConfirmationScope(regenerate ? scope : null);
 }, [attachments, changeMode, messages, models, releaseAttachments, scope, setAttachments, setDraft, t]);
 React.useEffect(() => {
  if (!pending || pending.scope !== scope || !ready || composeMode !== "image" || selectedModel !== pending.model) return;
  setOptions(pending.options);
  setPending(null);
 }, [composeMode, pending, ready, scope, selectedModel, setOptions]);
 return { reuse, applying: pending?.scope === scope, confirmationOpen: confirmationScope === scope,
  closeConfirmation: () => setConfirmationScope(null) };
}
