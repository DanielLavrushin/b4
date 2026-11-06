export interface Capture {
  protocol: string;
  domain: string;
  timestamp: string;
  size: number;
  filepath: string;
  hex_data: string;
  used_by?: string[];
}

export interface CaptureProbeResponse {
  success: boolean;
  message: string;
  already_captured?: boolean;
  errors?: string[];
}

export interface CaptureUploadResponse {
  success: boolean;
  message: string;
  size: number;
  domain: string;
  protocol: string;
}
