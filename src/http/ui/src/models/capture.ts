export type PayloadProtocol = "tls" | "quic";

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
}

export interface CaptureUploadResponse {
  success: boolean;
  message: string;
  size: number;
  domain: string;
  protocol: string;
  facts?: PayloadFacts | null;
}
