"use client";

import { useTranslations } from "next-intl";
import type { ImagePointQuote, PointWallet } from "@/entities/points";

// 区分余额、预留和实际消费，不把原美元用量展示为积分收费。
export function ChatPointsSummary({ wallet, quote, errorCode, loading }: {
  wallet: PointWallet | null; quote: ImagePointQuote | null; errorCode: string; loading: boolean;
}) {
  const t = useTranslations("chat.points");
  const errorKey = errorCode.replace("points.", "");
  return <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-5 pb-2 text-xs text-muted-foreground" aria-live="polite">
    {wallet ? <span title={t("walletDetails", { permanent: wallet.permanentPoints, daily: wallet.dailyPoints, frozen: wallet.frozenPoints })}>{t("balance", { points: wallet.totalPoints })}</span> : null}
    {loading ? <span>{t("quoting")}</span> : null}
    {quote ? <span className={wallet && wallet.totalPoints < quote.totalPoints ? "text-destructive" : ""}>{t("estimate", { points: quote.totalPoints, count: quote.count })}</span> : null}
    {errorCode ? <span role="alert" className="text-destructive">{t.has(`errors.${errorKey}`) ? t(`errors.${errorKey}`) : t("errors.unavailable")}</span> : null}
    {quote && wallet && wallet.totalPoints < quote.totalPoints ? <span className="text-destructive">{t("errors.insufficient")}</span> : null}
  </div>;
}
