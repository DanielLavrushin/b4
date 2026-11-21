export type PayloadProtocol = "tls" | "quic";

export const PAYLOAD_NAME_MAX_LENGTH = 200;

export type PayloadKind =
  | "tls_client_hello"
  | "quic_initial"
  | "stun"
  | "dtls_client_hello"
  | "other";

export type PayloadWarning =
  | "missing"
  | "empty"
  | "too_large"
  | "unrecognized"
  | "malformed"
  | "kind_mismatch";

export interface PayloadFacts {
  kind: PayloadKind;
  quic_version?: number;
  sni?: string;
  extensions?: number;
  ech?: boolean;
  fits_one_packet?: boolean;
  faults?: string[] | null;
}

export type PayloadOrigin = "generated" | "uploaded" | "imported" | "";

export interface Capture {
  protocol: string;
  domain: string;
  timestamp: string;
  size: number;
  filepath: string;
  origin?: PayloadOrigin;
  facts?: PayloadFacts | null;
  warnings?: PayloadWarning[] | null;
  missing?: boolean;
  used_by?: string[] | null;
}

export interface PayloadFileResolution {
  file: string;
  listed: boolean;
  exists: boolean;
  size: number;
  facts?: PayloadFacts | null;
}

export type BuiltinPayloadId = "google" | "vk" | "stun" | "quic1" | "quic2";

export type BuiltinPayloadSource =
  | "youtubeunblock"
  | "youtubeunblock_derived"
  | "zapret"
  | "";

export interface BuiltinPayload {
  id: BuiltinPayloadId | (string & {});
  protocol: PayloadProtocol;
  size: number;
  facts?: PayloadFacts | null;
  source: BuiltinPayloadSource | (string & {});
}

export interface PayloadInspection {
  size: number;
  facts?: PayloadFacts | null;
  protocol: PayloadProtocol | "";
  name: string;
  fits?: Record<PayloadProtocol, boolean>;
  limits?: Record<PayloadProtocol | "file", number>;
}

export interface CaptureGenerateResponse {
  success: boolean;
  message: string;
  already_captured?: boolean;
  errors?: string[];
  size?: number;
  domain: string;
  reloaded?: string[] | null;
  reload_error?: string;
}

export interface CaptureChangeResponse {
  success: boolean;
  message: string;
  reloaded?: string[] | null;
  reload_error?: string;
}

export interface CaptureRegenerateResponse {
  success: boolean;
  message: string;
  size: number;
  domain: string;
  facts?: PayloadFacts | null;
  reloaded?: string[] | null;
  reload_error?: string;
}

export interface CaptureUploadResponse {
  success: boolean;
  message: string;
  size: number;
  domain: string;
  protocol: string;
  facts?: PayloadFacts | null;
  reloaded?: string[] | null;
  reload_error?: string;
}
