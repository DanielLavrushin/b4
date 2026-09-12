import { apiPost } from "./apiClient";
import { B4SetConfig } from "@models/config";
import {
  HubEnvelope,
  HubEnvelopeResponse,
  HubImportResponse,
} from "@models/hub";

export const hubApi = {
  buildEnvelope: (set: B4SetConfig) =>
    apiPost<HubEnvelopeResponse>("/api/hub/envelope", set),
  importEnvelope: (envelope: HubEnvelope) =>
    apiPost<HubImportResponse>("/api/hub/import", envelope),
};
