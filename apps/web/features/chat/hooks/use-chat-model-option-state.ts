"use client";

import * as React from "react";
import {
  readCachedModelOptions,
  removeCachedModelOptions,
  writeCachedModelOptions,
} from "@/features/chat/model/chat-model-options-cache";
import {
  cloneConversationOptions,
  isConversationOptionsObject,
  sanitizeConversationOptions,
} from "@/features/chat/model/conversation-options";
import type { ChatModelOption } from "@/features/chat/types/chat-runtime";
import type { ConversationOptions } from "@/shared/api/conversation-types";

/**
 * Parameter state for the selected model: on model switch, initialize from local cache first, else model defaults;
 * user edits are written to localStorage; follows model default changes when the user hasn't edited; supports reset and restoring backend defaults.
 */
export function useChatModelOptionState({
  selectedModel,
  selectedPlatformModelName,
  chatPreferencesLoaded,
  reuseModelOptions,
  refreshModelOption,
  storageScope = "",
}: {
  storageScope?: string;
  selectedModel: ChatModelOption | null;
  selectedPlatformModelName: string;
  chatPreferencesLoaded: boolean;
  reuseModelOptions: boolean;
  refreshModelOption: (platformModelName: string) => Promise<ChatModelOption | null>;
}) {
  const [options, setOptions] = React.useState<ConversationOptions>({});
  const initializedOptionsModelRef = React.useRef("");
  const selectedModelDefaultOptionsRef = React.useRef<ConversationOptions>({});

  React.useEffect(() => {
    const modelName = selectedModel?.platformModelName.trim() || "";
    const platformModelName = modelName ? `${storageScope}:${modelName}` : "";
    if (!selectedModel || !platformModelName) {
      initializedOptionsModelRef.current = "";
      selectedModelDefaultOptionsRef.current = {};
      setOptions({});
      return;
    }
    if (!chatPreferencesLoaded) {
      return;
    }
    const nextDefaultOptions = cloneConversationOptions(selectedModel.defaultOptions);
    const previousDefaultOptions = selectedModelDefaultOptionsRef.current;
    if (initializedOptionsModelRef.current !== platformModelName) {
      const previousKey = initializedOptionsModelRef.current;
      const inheritNew = previousKey.includes(":new:") && !platformModelName.includes(":new:") && previousKey.endsWith(`:${selectedModel.composeMode}:${modelName}`);
      initializedOptionsModelRef.current = platformModelName;
      selectedModelDefaultOptionsRef.current = nextDefaultOptions;
      // 同一会话的聊天/生图参数始终记忆；全局复用偏好仍适用于未指定会话作用域的调用方。
      const cachedOptions = (reuseModelOptions || Boolean(storageScope)) ? (readCachedModelOptions(platformModelName) ?? (inheritNew ? readCachedModelOptions(previousKey) : null)) : null;
      if (inheritNew && cachedOptions) writeCachedModelOptions(platformModelName, cachedOptions);
      setOptions(cloneConversationOptions(cachedOptions ?? nextDefaultOptions));
      return;
    }
    selectedModelDefaultOptionsRef.current = nextDefaultOptions;
    const previousDefaultOptionsJSON = JSON.stringify(previousDefaultOptions);
    if (previousDefaultOptionsJSON === JSON.stringify(nextDefaultOptions)) {
      return;
    }
    setOptions((currentOptions) => {
      if (JSON.stringify(currentOptions) !== previousDefaultOptionsJSON) {
        return currentOptions;
      }
      removeCachedModelOptions(platformModelName);
      return cloneConversationOptions(nextDefaultOptions);
    });
  }, [chatPreferencesLoaded, reuseModelOptions, selectedModel, storageScope]);

  const setModelOptions = React.useCallback(
    (action: React.SetStateAction<ConversationOptions>) => {
      setOptions((previous) => {
        const next = typeof action === "function" ? action(previous) : action;
        const normalized = isConversationOptionsObject(next) ? sanitizeConversationOptions(next) : {};
        const modelName = selectedModel?.platformModelName.trim() || "";
    const platformModelName = modelName ? `${storageScope}:${modelName}` : "";
        if (platformModelName) {
          writeCachedModelOptions(platformModelName, normalized);
        }
        return normalized;
      });
    },
    [selectedModel?.platformModelName, storageScope],
  );

  const resetModelOptions = React.useCallback(
    (defaults?: ConversationOptions) => {
      const modelName = selectedModel?.platformModelName.trim() || "";
    const platformModelName = modelName ? `${storageScope}:${modelName}` : "";
      const nextDefaults = cloneConversationOptions(defaults ?? selectedModel?.defaultOptions ?? {});
      if (platformModelName) {
        removeCachedModelOptions(platformModelName);
      }
      setOptions(nextDefaults);
    },
    [selectedModel, storageScope],
  );

  const restoreBackendDefaultModelOptions = React.useCallback(async () => {
    const platformModelName = selectedModel?.platformModelName.trim() || selectedPlatformModelName.trim();
    if (!platformModelName) {
      return null;
    }
    const refreshedModel = await refreshModelOption(platformModelName);
    return refreshedModel ? cloneConversationOptions(refreshedModel.defaultOptions) : null;
  }, [refreshModelOption, selectedModel?.platformModelName, selectedPlatformModelName]);

  return {
    options,
    setModelOptions,
    resetModelOptions,
    restoreBackendDefaultModelOptions,
  };
}
