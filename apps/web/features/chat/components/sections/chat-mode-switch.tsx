"use client";

import { useTranslations } from "next-intl";
import { Button } from "@/components/ui/button";
import type { ChatComposeMode } from "@/features/chat/model/image-parameter-policy";

// 两种模式共用会话与输入框，仅改变本次任务和可选模型。
export function ChatModeSwitch({ mode, onChange, imageAvailable, disabled }: {
  mode: ChatComposeMode; onChange: (mode: ChatComposeMode) => void; imageAvailable: boolean; disabled: boolean;
}) {
  const t = useTranslations("chat.points");
  return (
    <div role="group" aria-label={t("modeLabel")} className="flex items-center gap-0.5 rounded-lg bg-muted/60 p-0.5">
      {(["chat", "image"] as const).map((value) => (
        <Button key={value} type="button" size="sm" variant={mode === value ? "secondary" : "ghost"}
          className="h-7 rounded-md px-2 text-xs" aria-pressed={mode === value}
          title={value === "image" && !imageAvailable ? t("noImageModel") : undefined}
          disabled={disabled || (value === "image" && !imageAvailable)} onClick={() => onChange(value)}>{t(value)}</Button>
      ))}
    </div>
  );
}
