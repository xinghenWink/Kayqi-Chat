"use client";

import { useTranslations } from "next-intl";
import * as React from "react";
import { toast } from "sonner";
import { toPendingAttachment } from "@/features/chat/model/message-submit";
import type { ChatModelOption, PendingAttachment } from "@/features/chat/types/chat-runtime";
import type { MessageAttachment } from "@/features/chat/types/messages";
import type { FileObjectDTO } from "@/shared/api/file-types";

/**
 * Reusing generated media: attach generated images back to the composer, auto-switching to a capable model if the current one can't edit images;
 * set a generated video as extension input and switch to a model that supports video extension; attach history files (with count limit checks).
 */
export function useChatMediaAttachmentActions({
  attachments,
  maxFilesPerMessage,
  modelOptions,
  selectedModel,
  selectedPlatformModelName,
  setAttachments,
  setSelectedPlatformModelName,
  releaseAttachments,
  enterImageMode,
}: {
  enterImageMode?: (mode: "image", preferredModel?: string) => boolean;
  attachments: PendingAttachment[];
  maxFilesPerMessage: number;
  modelOptions: ChatModelOption[];
  selectedModel: ChatModelOption | null;
  selectedPlatformModelName: string;
  setAttachments: React.Dispatch<React.SetStateAction<PendingAttachment[]>>;
  setSelectedPlatformModelName: (platformModelName: string) => void;
  releaseAttachments: (items: PendingAttachment[]) => void;
}) {
  const t = useTranslations("chat");

  const onEditGeneratedImageAttachment = React.useCallback(
    (attachment: MessageAttachment, sourceModelName?: string) => {
      const editingModel = (selectedModel?.kinds.includes("image_edit") && selectedModel.imageParameters ? selectedModel : undefined)
        ?? modelOptions.find((model) => model.platformModelName === sourceModelName && model.kinds.includes("image_edit") && model.imageParameters)
        ?? modelOptions.find((model) => model.kinds.includes("image_edit") && model.imageParameters);
      if (!editingModel || (enterImageMode && !enterImageMode("image", editingModel.platformModelName))) {
        toast.error(t("points.noEditModel")); return;
      }
      const referenceLimit = editingModel.imageParameters?.maxReferenceImages ?? maxFilesPerMessage;
      if (referenceLimit > 0 && !attachments.some((item) => item.fileID === attachment.fileID) && attachments.length >= referenceLimit) {
        toast.error(t("attachments.limitReached")); return;
      }
      const alreadyAttached = attachments.some((item) => item.fileID === attachment.fileID);
      if (!alreadyAttached && maxFilesPerMessage > 0 && attachments.length >= maxFilesPerMessage) {
        toast.error(t("attachments.limitReached"), {
          description: t("attachments.maxUploadFiles", { count: maxFilesPerMessage }),
        });
        return;
      }

      const pendingAttachment = toPendingAttachment(attachment);
      setAttachments((previous) => {
        if (previous.some((item) => item.fileID === pendingAttachment.fileID)) {
          return previous;
        }
        return [...previous, pendingAttachment];
      });


    },
    [
      attachments,
      enterImageMode,
      maxFilesPerMessage,
      modelOptions,
      selectedModel,
      setAttachments,
      setSelectedPlatformModelName,
      t,
    ],
  );

  const onExtendGeneratedVideoAttachment = React.useCallback(
    (attachment: MessageAttachment, sourceModelName?: string) => {
      const normalizedSourceModelName = sourceModelName?.trim() || "";
      const sourceModel = modelOptions.find(
        (item) =>
          item.platformModelName === normalizedSourceModelName &&
          item.videoExtension?.enabled,
      );
      const extensionModel =
        sourceModel ??
        (selectedModel?.videoExtension?.enabled ? selectedModel : undefined) ??
        modelOptions.find((item) => item.videoExtension?.enabled);

      if (!extensionModel) {
        toast.error(t("submit.mediaMode.blockedDescriptions.video_extension_unsupported"));
        return;
      }

      releaseAttachments(attachments);
      setAttachments([toPendingAttachment(attachment)]);
      if (extensionModel.platformModelName !== selectedPlatformModelName) {
        setSelectedPlatformModelName(extensionModel.platformModelName);
      }
    },
    [
      attachments,
      modelOptions,
      releaseAttachments,
      selectedModel,
      selectedPlatformModelName,
      setAttachments,
      setSelectedPlatformModelName,
      t,
    ],
  );

  const onAttachExistingFile = React.useCallback(
    (file: FileObjectDTO) => {
      const alreadyAttached = attachments.some((item) => item.fileID === file.fileID);
      if (alreadyAttached) {
        return;
      }
      if (maxFilesPerMessage > 0 && attachments.length >= maxFilesPerMessage) {
        toast.error(t("attachments.limitReached"), {
          description: t("attachments.maxUploadFiles", { count: maxFilesPerMessage }),
        });
        return;
      }
      setAttachments((previous) => {
        if (previous.some((item) => item.fileID === file.fileID)) {
          return previous;
        }
        return [
          ...previous,
          {
            fileID: file.fileID,
            fileName: file.fileName,
            mimeType: file.mimeType,
            detectedMime: file.detectedMIME,
            fileCategory: file.fileCategory,
            sizeBytes: file.sizeBytes,
            processingStatus: file.processingStatus,
            processingReady: file.processingReady,
            processingErrorCode: file.processingErrorCode,
            processingErrorMessage: file.processingErrorMessage,
            extractStatus: file.extractStatus,
            embedStatus: file.embedStatus,
            ragReady: false,
            ragReason: "",
            ocrUsed: false,
            ragOptOut: file.ragOptOut,
          },
        ];
      });
    },
    [attachments, maxFilesPerMessage, setAttachments, t],
  );

  return {
    onEditGeneratedImageAttachment,
    onExtendGeneratedVideoAttachment,
    onAttachExistingFile,
  };
}
