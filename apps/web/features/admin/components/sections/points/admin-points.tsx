"use client";
import { useTranslations } from "next-intl";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { SettingsPage, SettingsSection, SettingsFieldList, SettingsFieldRow } from "@/shared/components/settings-layout";
import { createSecureUUID } from "@/shared/lib/secure-id";
import { useAdminPoints } from "@/features/admin/hooks/use-admin-points";

const SETTING_LIMITS = {
  signup_bonus: [0, 1000000000], daily_bonus: [0, 1000000000], image_max_count: [1, 4],
  image_max_reference_images: [1, 16], image_concurrent_per_user: [1, 1], image_concurrent_global: [1, 16],
} as const;

// 积分后台沿用现有设置、表格和鉴权组件，所有写操作由服务端再次校验。
export function AdminPointsPage() {
  const t = useTranslations("adminUsers.points");
  const {priceDraft,setPriceDraft,createPrice,busy,error,settings,setSettings,prices,setPrices,models,selectedModel,qualities,setQualities,sizes,setSizes,maxCount,setMaxCount,maxReferences,setMaxReferences,stream,setStream,userID,setUserID,wallet,setWallet,delta,setDelta,reason,setReason,adjustmentKey,page,records,reservations,chosen,selectModel,saveSettings,saveCapabilities,addDefaultPrices,queryWallet,adjust,loadRecords,savePrice}=useAdminPoints();
  return <SettingsPage>
    <SettingsSection title={t("title")}>
      <p className="text-sm text-muted-foreground">{t("description")}</p>
      {error ? <p role="alert" className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive">{error}</p> : null}
      <Tabs defaultValue="rules">
        <TabsList className="max-w-full overflow-x-auto">{(["rules", "prices", "wallet", "records"] as const).map((tab) => <TabsTrigger key={tab} value={tab}>{t(tab)}</TabsTrigger>)}</TabsList>
        <TabsContent value="rules" className="space-y-5 pt-4">
          <SettingsFieldList>
            <SettingsFieldRow title={t("enabled")} description={t("enabledDescription")}><Switch aria-label={t("enabled")} checked={settings.enabled === "true"} disabled={busy} onCheckedChange={(checked) => setSettings((current) => ({ ...current, enabled: String(checked) }))} /></SettingsFieldRow>
            {Object.entries(SETTING_LIMITS).map(([key, [min, max]]) => <SettingsFieldRow key={key} title={t(`settings.${key}`)}><Input type="number" aria-label={t(`settings.${key}`)} min={min} max={max} value={settings[key] ?? ""} disabled={busy} onChange={(event) => setSettings((current) => ({ ...current, [key]: event.target.value }))} /></SettingsFieldRow>)}
          </SettingsFieldList>
          <p className="text-xs text-muted-foreground">{t("dailyDescription")}</p>
          <Button disabled={busy || !Object.keys(settings).length} onClick={() => void saveSettings()}>{t("saveRules")}</Button>
        </TabsContent>
        <TabsContent value="prices" className="space-y-5 pt-4">
          <SettingsSection title={t("verifiedModel")}>
            <Select value={selectedModel} onValueChange={selectModel}><SelectTrigger aria-label={t("chooseModel")}><SelectValue placeholder={t("chooseModel")} /></SelectTrigger><SelectContent>{models.map((model) => <SelectItem key={model.id} value={model.platformModelName}>{model.platformModelName}</SelectItem>)}</SelectContent></Select>
            <p className="text-xs text-muted-foreground">{t("verifyDescription")}</p>
            <SettingsFieldList>
              <SettingsFieldRow title={t("qualities")}><Input aria-label={t("qualities")} value={qualities} onChange={(event) => setQualities(event.target.value)} /></SettingsFieldRow>
              <SettingsFieldRow title={t("sizes")}><Input aria-label={t("sizes")} value={sizes} onChange={(event) => setSizes(event.target.value)} /></SettingsFieldRow>
              <SettingsFieldRow title={t("maxCount")}><Input aria-label={t("maxCount")} type="number" min={1} max={4} value={maxCount} onChange={(event) => setMaxCount(Number(event.target.value))} /></SettingsFieldRow>
              <SettingsFieldRow title={t("maxReferences")}><Input aria-label={t("maxReferences")} type="number" min={0} max={16} value={maxReferences} onChange={(event) => setMaxReferences(Number(event.target.value))} /></SettingsFieldRow>
              <SettingsFieldRow title={t("stream")}><Switch aria-label={t("stream")} checked={stream} onCheckedChange={setStream} /></SettingsFieldRow>
            </SettingsFieldList>
            <div className="flex flex-wrap gap-2"><Button disabled={busy || !chosen} onClick={() => void saveCapabilities()}>{t("saveCapabilities")}</Button><Button variant="outline" disabled={busy || !chosen} onClick={() => void addDefaultPrices()}>{t("addDefaultPrices")}</Button></div>
          </SettingsSection>
          <SettingsSection title={t("createPrice")}>
            <div className="grid gap-3 sm:grid-cols-2">
              <Select value={priceDraft.taskType} onValueChange={(value) => { if (value === "image_generation" || value === "image_edit") setPriceDraft((current) => ({ ...current, taskType: value })); }}><SelectTrigger aria-label={t("task")}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="image_generation">{t("generate")}</SelectItem><SelectItem value="image_edit">{t("edit")}</SelectItem></SelectContent></Select>
              <Input aria-label={t("quality")} value={priceDraft.quality} placeholder={t("quality")} onChange={(event) => setPriceDraft((current) => ({ ...current, quality: event.target.value }))} />
              <Input aria-label={t("size")} value={priceDraft.size} placeholder={t("sizeWildcard")} onChange={(event) => setPriceDraft((current) => ({ ...current, size: event.target.value }))} />
              <Input aria-label={t("unitPrice")} type="number" min={0} max={1000000000} value={priceDraft.pointsPerImage} onChange={(event) => setPriceDraft((current) => ({ ...current, pointsPerImage: Number(event.target.value) }))} />
            </div>
            <Button variant="outline" disabled={busy || !chosen || !priceDraft.quality} onClick={() => void createPrice()}>{t("createPrice")}</Button>
          </SettingsSection>
          <Table><TableHeader><TableRow>{["model", "task", "quality", "size", "unitPrice", "enabled", "actions"].map((key) => <TableHead key={key}>{t(key)}</TableHead>)}</TableRow></TableHeader><TableBody>
            {prices.map((price) => <TableRow key={price.id}><TableCell>{price.model}</TableCell><TableCell>{t(price.taskType === "image_edit" ? "edit" : "generate")}</TableCell><TableCell>{price.quality}</TableCell><TableCell>{price.size || t("allSizes")}</TableCell>
              <TableCell><Input aria-label={`${price.model} ${price.quality} ${t("unitPrice")}`} className="w-24" type="number" min={0} max={1000000000} value={price.pointsPerImage} onChange={(event) => setPrices((current) => current.map((row) => row.id === price.id ? { ...row, pointsPerImage: Number(event.target.value) } : row))} /></TableCell>
              <TableCell><Switch aria-label={`${price.model} ${price.quality} ${t("enabled")}`} checked={price.enabled} onCheckedChange={(enabled) => setPrices((current) => current.map((row) => row.id === price.id ? { ...row, enabled } : row))} /></TableCell>
              <TableCell><Button size="sm" variant="outline" disabled={busy} onClick={() => void savePrice(price)}>{t("save")}</Button></TableCell></TableRow>)}
          </TableBody></Table>
          {!prices.length ? <p className="text-sm text-muted-foreground">{t("noPrices")}</p> : null}
        </TabsContent>
        <TabsContent value="wallet" className="space-y-5 pt-4">
          <div className="flex gap-2"><Input aria-label={t("userID")} type="number" min={1} value={userID} placeholder={t("userID")} onChange={(event) => { setUserID(event.target.value); setWallet(null); adjustmentKey.current = createSecureUUID(); }} /><Button variant="outline" disabled={busy || !Number(userID)} onClick={() => void queryWallet()}>{t("query")}</Button></div>
          {wallet ? <p className="rounded-xl bg-muted/50 p-4 text-sm">{t("walletSummary", { user: wallet.userID, permanent: wallet.permanentPoints, daily: wallet.dailyPoints, frozen: wallet.frozenPoints })}</p> : null}
          <Input aria-label={t("adjustDelta")} type="number" value={delta} placeholder={t("adjustDelta")} onChange={(event) => { setDelta(event.target.value); adjustmentKey.current = createSecureUUID(); }} />
          <Input aria-label={t("adjustReason")} value={reason} maxLength={255} placeholder={t("adjustReason")} onChange={(event) => { setReason(event.target.value); adjustmentKey.current = createSecureUUID(); }} />
          <p className="text-xs text-muted-foreground">{t("adjustDescription")}</p>
          <Button disabled={busy || !Number(userID) || !Number(delta) || !reason.trim()} onClick={() => void adjust()}>{t("adjust")}</Button>
        </TabsContent>
        <TabsContent value="records" className="space-y-5 pt-4">
          <div className="flex gap-2"><Input aria-label={t("userFilter")} type="number" min={1} value={userID} placeholder={t("userFilter")} onChange={(event) => setUserID(event.target.value)} /><Button disabled={busy} onClick={() => void loadRecords(1)}>{t("query")}</Button></div>
          <SettingsSection title={t("transactions")}><Table><TableHeader><TableRow>{["userID", "type", "dailyDelta", "permanentDelta", "reason", "createdAt"].map((key) => <TableHead key={key}>{t(key)}</TableHead>)}</TableRow></TableHeader><TableBody>{records.results.map((row) => <TableRow key={row.id}><TableCell>{row.userID}</TableCell><TableCell>{t.has(`types.${row.type}`) ? t(`types.${row.type}`) : row.type}</TableCell><TableCell>{row.dailyDelta}</TableCell><TableCell>{row.permanentDelta}</TableCell><TableCell>{row.reason}</TableCell><TableCell>{row.createdAt}</TableCell></TableRow>)}</TableBody></Table></SettingsSection>
          <SettingsSection title={t("reservations")}><Table><TableHeader><TableRow>{["run", "userID", "status", "delivered", "spent"].map((key) => <TableHead key={key}>{t(key)}</TableHead>)}</TableRow></TableHeader><TableBody>{reservations.results.map((row) => <TableRow key={row.runID}><TableCell className="max-w-44 truncate" title={row.runID}>{row.runID}</TableCell><TableCell>{row.userID}</TableCell><TableCell>{t.has(`types.${row.status}`) ? t(`types.${row.status}`) : row.status}</TableCell><TableCell>{row.deliveredCount}/{row.requestedCount}</TableCell><TableCell>{row.spentPoints}</TableCell></TableRow>)}</TableBody></Table></SettingsSection>
          <div className="flex items-center justify-end gap-3"><Button variant="outline" disabled={busy || page <= 1} onClick={() => void loadRecords(page - 1)}>{t("previous")}</Button><span className="text-sm text-muted-foreground">{page}</span><Button variant="outline" disabled={busy || page * 20 >= Math.max(records.total, reservations.total)} onClick={() => void loadRecords(page + 1)}>{t("next")}</Button></div>
        </TabsContent>
      </Tabs>
    </SettingsSection>
  </SettingsPage>;
}
