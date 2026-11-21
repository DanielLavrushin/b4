export interface Capture {
  protocol: string;
  domain: string;
  timestamp: string;
  size: number;
  filepath: string;
  missing?: boolean;
  used_by?: string[];
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
}
