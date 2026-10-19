import type { AdjustmentRequest, Price, PriceRequest, Quote, QuoteRequest, ReservationPage, TransactionPage, Wallet } from "@deeix/api-contract";
import { authedRequest } from "@/shared/api/authed-client";

// 所有字段来自生成契约；客户端不保存或计算最终单价。
export type PointWallet = Wallet;
export type ImagePointQuote = Quote;
export type ImagePointPrice = Price;
export type ImagePointPriceRequest = PriceRequest;

export const getPointWallet = (accessToken: string, signal?: AbortSignal) =>
  authedRequest<Wallet>("/api/v1/points/me", { accessToken, signal });
export const ensureDailyPoints = (accessToken: string, signal?: AbortSignal) =>
  authedRequest<Wallet>("/api/v1/points/daily-grants/ensure", { accessToken, method: "POST", signal });
export const quoteImagePoints = (accessToken: string, body: QuoteRequest, signal?: AbortSignal) =>
  authedRequest<Quote>("/api/v1/points/image-quote", { accessToken, method: "POST", body, signal });
export const listPointPrices = (accessToken: string) =>
  authedRequest<Price[]>("/api/v1/admin/points/prices", { accessToken }, true);
export const savePointPrice = (accessToken: string, body: PriceRequest) =>
  authedRequest<Price>("/api/v1/admin/points/prices", { accessToken, method: "PUT", body }, true);
export const getUserPointWallet = (accessToken: string, userID: number) =>
  authedRequest<Wallet>(`/api/v1/admin/points/users/${userID}`, { accessToken }, true);
export const adjustUserPoints = (accessToken: string, userID: number, body: AdjustmentRequest) =>
  authedRequest<Wallet>(`/api/v1/admin/points/users/${userID}/adjust`, { accessToken, method: "POST", body }, true);
export const listPointTransactions = (accessToken: string, page = 1, userID?: number, admin = false) =>
  authedRequest<TransactionPage>(`/api/v1/${admin ? "admin/" : ""}points/transactions?page=${page}&page_size=20${userID ? `&user_id=${userID}` : ""}`, { accessToken }, admin);
export const listPointReservations = (accessToken: string, page = 1, userID?: number) =>
  authedRequest<ReservationPage>(`/api/v1/admin/points/reservations?page=${page}&page_size=20${userID ? `&user_id=${userID}` : ""}`, { accessToken }, true);

// 完成事件只触发余额刷新，余额事实始终来自服务端。
export function notifyPointsChanged() {
  window.dispatchEvent(new Event("kayqi-points-changed"));
}
