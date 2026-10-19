"use client";
import * as React from "react";
import { useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import type { ReservationPage, TransactionPage } from "@deeix/api-contract";
import { listAdminSettingsByNamespace, patchAdminSettings } from "@/features/admin/api/settings";
import { listAdminLLMModels, updateAdminLLMModel } from "@/features/admin/api/llm";
import type { AdminLLMModelDTO } from "@/features/admin/api/llm-types";
import { adjustUserPoints, getUserPointWallet, listPointPrices, listPointReservations, listPointTransactions, savePointPrice, type ImagePointPrice, type PointWallet } from "@/entities/points";
import { parseImageParameterPolicy } from "@/entities/model";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { resolveLocalizedErrorMessage } from "@/i18n/resolve-error-message";
import { createSecureUUID } from "@/shared/lib/secure-id";
import { isRecord } from "@/shared/lib/type-guards";

// 所有 I/O 留在 hook，页面组件只负责展示与用户交互。
export function useAdminPoints() {
  const t = useTranslations("adminUsers.points");
  const search = useSearchParams();
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [settings, setSettings] = React.useState<Record<string, string>>({});
  const [prices, setPrices] = React.useState<ImagePointPrice[]>([]);
  const [models, setModels] = React.useState<AdminLLMModelDTO[]>([]);
  const [selectedModel, setSelectedModel] = React.useState("");
  const [qualities, setQualities] = React.useState("low,medium,high");
  const [sizes, setSizes] = React.useState("1024x1024");
  const [maxCount, setMaxCount] = React.useState(1);
  const [maxReferences, setMaxReferences] = React.useState(1);
  const [stream, setStream] = React.useState(false);
  const [priceDraft, setPriceDraft] = React.useState<{ taskType: "image_generation" | "image_edit"; quality: string; size: string; pointsPerImage: number }>({ taskType: "image_generation", quality: "low", size: "", pointsPerImage: 5 });
  const [userID, setUserID] = React.useState(search.get("user_id") || "");
  const [wallet, setWallet] = React.useState<PointWallet | null>(null);
  const [delta, setDelta] = React.useState("");
  const [reason, setReason] = React.useState("");
  const adjustmentKey = React.useRef(createSecureUUID());
  const [page, setPage] = React.useState(1);
  const [records, setRecords] = React.useState<TransactionPage>({ results: [], total: 0 });
  const [reservations, setReservations] = React.useState<ReservationPage>({ results: [], total: 0 });

  const load = React.useCallback(async (token: string) => {
    const [items, priceRows, modelPage] = await Promise.all([
      listAdminSettingsByNamespace(token, "points"), listPointPrices(token), listAdminLLMModels(token, { pageSize: 1000 }),
    ]);
    setSettings(Object.fromEntries(items.map((item) => [item.key, item.value])));
    setPrices(priceRows);
    setModels(modelPage.results.filter((model) => /image_gen|image_edit/.test(model.kindsJSON)));
  }, []);

  const perform = React.useCallback(async (operation: (token: string) => Promise<void>) => {
    setBusy(true); setError("");
    try { const token = await resolveAccessToken(); if (!token) throw new Error(t("loginRequired")); await operation(token); }
    catch (failure) { setError(resolveLocalizedErrorMessage(failure)); }
    finally { setBusy(false); }
  }, [t]);

  React.useEffect(() => { void perform(load); }, [load, perform]);

  const chosen = models.find((model) => model.platformModelName === selectedModel);
  const selectModel = (name: string) => {
    setSelectedModel(name);
    const model = models.find((item) => item.platformModelName === name);
    const policy = model ? parseImageParameterPolicy(model.capabilitiesJSON) : null;
    setQualities(policy?.qualities.join(",") || "low,medium,high"); setSizes(policy?.sizes.join(",") || "1024x1024");
    setMaxCount(policy?.maxCount || 1); setMaxReferences(policy?.maxReferenceImages ?? 1);
    try { const caps: unknown = JSON.parse(model?.capabilitiesJSON || "{}"); setStream(isRecord(caps) && isRecord(caps.image) && caps.image.stream === true); }
    catch { setStream(false); }
  };

  const saveSettings = () => perform(async (token) => {
    const items = Object.entries(settings).map(([key, value]) => ({ namespace: "points", key, value }));
    // 开启时明确选择自用计量，避免图片同时消耗原货币余额。
    if (settings.enabled === "true") items.push({ namespace: "billing", key: "mode", value: "self" });
    await patchAdminSettings(token, { items }); await load(token); toast.success(t("saved"));
  });

  const saveCapabilities = () => perform(async (token) => {
    if (!chosen) throw new Error(t("chooseModel"));
    const parsed: unknown = JSON.parse(chosen.capabilitiesJSON || "{}");
    if (!isRecord(parsed)) throw new Error(t("invalidCapabilities"));
    const unique = (text: string) => [...new Set(text.split(/[,，\s]+/).map((value) => value.trim()).filter(Boolean))];
    const allowedQualities = unique(qualities); const allowedSizes = unique(sizes);
    if (!allowedQualities.length || !allowedSizes.length || !Number.isInteger(maxCount) || maxCount < 1 || maxCount > 4 || !Number.isInteger(maxReferences) || maxReferences < 0 || maxReferences > 16) throw new Error(t("invalidCapabilities"));
    const image = isRecord(parsed.image) ? parsed.image : {};
    const capabilitiesJSON = JSON.stringify({ ...parsed, image: { ...image, stream, parameters: { qualities: allowedQualities, sizes: allowedSizes, maxCount, maxReferenceImages: maxReferences } } });
    await updateAdminLLMModel(token, chosen.id, { capabilitiesJSON }); await load(token); toast.success(t("saved"));
  });

  const addDefaultPrices = () => perform(async (token) => {
    if (!chosen) throw new Error(t("chooseModel"));
    const policy = parseImageParameterPolicy(chosen.capabilitiesJSON);
    if (!policy) throw new Error(t("verifyFirst"));
    const defaults: Record<string, number> = { low: 5, medium: 7, high: 10 };
    const tasks = (["image_generation", "image_edit"] as const).filter((task) => chosen.kindsJSON.includes(task === "image_generation" ? "image_gen" : "image_edit"));
    for (const taskType of tasks) for (const quality of policy.qualities) {
      if (prices.some((price) => price.model === chosen.platformModelName && price.taskType === taskType && price.quality === quality && price.size === "")) continue;
      await savePointPrice(token, { model: chosen.platformModelName, taskType, quality, size: "", pointsPerImage: defaults[quality] ?? 10, version: 0, enabled: true });
    }
    await load(token); toast.success(t("saved"));
  });

  const saveExistingPrice = async (token: string, price: ImagePointPrice) => {
    const taskType = price.taskType;
    if (taskType !== "image_generation" && taskType !== "image_edit") throw new Error(t("invalidCapabilities"));
    await savePointPrice(token, { ...price, taskType });
  };
  const queryWallet = () => perform(async (token) => { setWallet(await getUserPointWallet(token, Number(userID))); });
  const adjust = () => perform(async (token) => {
    const amount = Number(delta);
    if (!Number.isSafeInteger(amount) || amount === 0 || !reason.trim()) throw new Error(t("adjustInvalid"));
    const value = await adjustUserPoints(token, Number(userID), { delta: amount, reason: reason.trim(), refNo: adjustmentKey.current });
    setWallet(value); setDelta(""); setReason(""); adjustmentKey.current = createSecureUUID(); toast.success(t("saved"));
  });
  const loadRecords = (nextPage: number) => perform(async (token) => {
    const filter = userID.trim() ? Number(userID) : undefined;
    const [transactions, runs] = await Promise.all([listPointTransactions(token, nextPage, filter, true), listPointReservations(token, nextPage, filter)]);
    setRecords(transactions); setReservations(runs); setPage(nextPage);
  });

  const createPrice = () => perform(async (token) => {
    if (!chosen || !Number.isSafeInteger(priceDraft.pointsPerImage) || priceDraft.pointsPerImage < 0) throw new Error(t("invalidCapabilities"));
    await savePointPrice(token, { ...priceDraft, model: chosen.platformModelName, version: 0, enabled: true });
    await load(token); toast.success(t("saved"));
  });
  const savePrice = (price: ImagePointPrice) => perform(async (token) => { await saveExistingPrice(token, price); await load(token); toast.success(t("saved")); });
  return { priceDraft,setPriceDraft,createPrice,busy,error,settings,setSettings,prices,setPrices,models,selectedModel,qualities,setQualities,sizes,setSizes,maxCount,setMaxCount,maxReferences,setMaxReferences,stream,setStream,userID,setUserID,wallet,setWallet,delta,setDelta,reason,setReason,adjustmentKey,page,records,reservations,chosen,selectModel,saveSettings,saveCapabilities,addDefaultPrices,queryWallet,adjust,loadRecords,savePrice };
}
