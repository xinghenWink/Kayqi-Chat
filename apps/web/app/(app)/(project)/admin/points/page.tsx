import { Suspense } from "react";
import { AdminPointsPage } from "@/features/admin";

// 查询参数只在客户端读取，保持静态导出与现有部署结构兼容。
export default function Page() {
  return <Suspense fallback={null}><AdminPointsPage /></Suspense>;
}
