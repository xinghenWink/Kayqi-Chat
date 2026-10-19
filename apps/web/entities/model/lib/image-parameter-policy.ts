import { isRecord } from "@/shared/lib/type-guards";

export type ImageParameterPolicy = { qualities: string[]; sizes: string[]; maxCount: number; maxReferenceImages: number };

// 只开放后台明确声明的能力，通用控件中的建议值不代表上游支持。
export function parseImageParameterPolicy(raw: string): ImageParameterPolicy | null {
  try {
    const value: unknown = JSON.parse(raw);
    const image = isRecord(value) && isRecord(value.image) ? value.image : null;
    const parameters = image && isRecord(image.parameters) ? image.parameters : null;
    if (!parameters) return null;
    const qualities = Array.isArray(parameters.qualities) ? parameters.qualities.filter((v): v is string => typeof v === "string" && v.length > 0) : [];
    const sizes = Array.isArray(parameters.sizes) ? parameters.sizes.filter((v): v is string => typeof v === "string" && v.length > 0) : [];
    const maxCount = Number(parameters.maxCount);
    const maxReferenceImages = Number(parameters.maxReferenceImages ?? 0);
    return qualities.length && sizes.length && Number.isInteger(maxCount) && maxCount >= 1
      ? { qualities, sizes, maxCount: Math.min(maxCount, 4), maxReferenceImages: Math.max(0, Math.min(maxReferenceImages, 16)) }
      : null;
  } catch { return null; }
}

