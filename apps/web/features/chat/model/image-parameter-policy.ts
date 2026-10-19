export { parseImageParameterPolicy, type ImageParameterPolicy } from "@/entities/model";
import type { ChatModelOption, ModelOptionControl } from "@/features/chat/types/chat-runtime";
import type { ConversationOptions } from "@/shared/api/conversation-types";

export type ChatComposeMode = "chat" | "image";
export function modelSupportsComposeMode(model: ChatModelOption, mode: ChatComposeMode, requirePolicy = true): boolean {
  if (mode === "image") return model.personalProviderName === null && (!requirePolicy || Boolean(model.imageParameters)) && model.kinds.some((kind) => kind === "image_gen" || kind === "image_edit");
  return model.kinds.length === 0 || model.kinds.some((kind) => ["chat", "audio", "video_gen"].includes(kind));
}

const IMAGE_OPTION_KEYS = ["quality", "size", "n", "background", "output_format", "output_compression", "input_fidelity", "partial_images", "moderation"];

// 为双能力模型隔离两种模式的参数，不把聊天工具带进图片请求。
export function optionsForComposeMode(options: ConversationOptions, mode: ChatComposeMode): ConversationOptions {
  const next = { ...options };
  if (mode === "chat") {
    for (const key of IMAGE_OPTION_KEYS) delete next[key];
    if (next.response_format === "url" || next.response_format === "b64_json") delete next.response_format;
  } else {
    for (const key of Object.keys(next)) if (!IMAGE_OPTION_KEYS.includes(key)) delete next[key];
  }
  return next;
}

export function modelForComposeMode(model: ChatModelOption, mode: ChatComposeMode): ChatModelOption {
  if (mode !== "image" || !model.imageParameters) return { ...model, composeMode: mode, defaultOptions: optionsForComposeMode(model.defaultOptions, mode) };
  const policy = model.imageParameters;
  const controls: ModelOptionControl[] = [
    { path: "quality", type: "select", options: policy.qualities },
    { path: "size", type: "select", options: policy.sizes },
    { path: "n", type: "select", options: Array.from({ length: policy.maxCount }, (_, i) => String(i + 1)) },
  ];
  return {
    ...model, composeMode: mode,
    defaultOptions: { quality: policy.qualities[0], size: policy.sizes[0], n: 1, ...optionsForComposeMode(model.defaultOptions, mode) },
    optionControls: controls, controls: [],
  };
}
