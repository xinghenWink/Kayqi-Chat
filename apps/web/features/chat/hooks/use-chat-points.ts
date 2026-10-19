"use client";

import * as React from "react";
import type { QuoteRequest } from "@deeix/api-contract";
import { ensureDailyPoints, getPointWallet, quoteImagePoints, type ImagePointQuote, type PointWallet } from "@/entities/points";
import type { PendingAttachment } from "@/features/chat/types/chat-runtime";
import type { ConversationOptions } from "@/shared/api/conversation-types";
import { ApiError } from "@/shared/api/http-client";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";

// 参数变动立即使旧报价失效；晚到的请求不能覆盖当前模型的报价。
export function useChatPoints({ active, scope, model, options, attachments }: {
  active: boolean; scope: string; model: string; options: ConversationOptions; attachments: PendingAttachment[];
}) {
  const [revision, refreshQuote] = React.useReducer((value: number) => value + 1, 0);
  const [walletState, setWallet] = React.useState<{ scope: string; value: PointWallet } | null>(null);
  const [quoteState, setQuote] = React.useState<{ key: string; value: ImagePointQuote } | null>(null);
  const [failure, setFailure] = React.useState<{ key: string; code: string } | null>(null);
  const fileIDs = React.useMemo(() => attachments.map((item) => item.fileID), [attachments]);
  const key = JSON.stringify([scope, model, options, fileIDs, revision]);
  const request = React.useMemo<QuoteRequest>(() => ({model,taskType:fileIDs.length ? "image_edit" : "image_generation",options,fileIDs}),[model,options,fileIDs]);

  React.useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    const refresh = async (grant: boolean) => {
      try {
        const token = await resolveAccessToken();
        if (!token || controller.signal.aborted) return;
        const value = await (grant ? ensureDailyPoints(token, controller.signal) : getPointWallet(token, controller.signal));
        if (!controller.signal.aborted) setWallet({ scope, value });
      } catch { /* 报价请求展示业务错误；余额刷新失败不覆盖已有报价。 */ }
    };
    void refresh(true);
    const onRefresh = () => { refreshQuote(); void refresh(true); };
    window.addEventListener("kayqi-points-changed", onRefresh);
    window.addEventListener("focus", onRefresh);
    return () => { controller.abort(); window.removeEventListener("kayqi-points-changed", onRefresh); window.removeEventListener("focus", onRefresh); };
  }, [active, scope]);

  React.useEffect(() => {
    if (!active || !request.model) return;
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      try {
        const token = await resolveAccessToken();
        if (!token || controller.signal.aborted) return;
        const value = await quoteImagePoints(token, request, controller.signal);
        if (!controller.signal.aborted) { setQuote({ key, value }); setFailure(null); }
      } catch (error) {
        if (!controller.signal.aborted) setFailure({ key, code: error instanceof ApiError ? error.errorCode || "points.unavailable" : "points.unavailable" });
      }
    }, 180);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [active, key, request]);

  const quote = active && quoteState?.key === key ? quoteState.value : null;
  const wallet = walletState?.scope === scope ? walletState.value : null;
  const errorCode = active && failure?.key === key ? failure.code : "";
  return { wallet, quote, errorCode, refreshQuote, loading: active && Boolean(model) && !quote && !errorCode };
}
